(* dedup -- AST-based automatic code deduplication ("extract-method"
   folding) for OCaml implementation files.

   Pipeline (all inside this one tool; the driver script handles the
   build-gate and the cross-file loop):

     1. Parse the file with Parse.implementation.
     2. Enumerate every candidate expression subtree (node count >=
        threshold, non-trivial) with its true source span and its
        enclosing top-level context.
     3. Normalize (strip all locations) + Marshal => structural key.
     4. Group by key; a clone class has >= 2 non-nested occurrences in
        >= 2 distinct top-level contexts (unless --same-context-ok).
     5. Fold the best class (score = nodes * occurrences): emit
        `let dedup_shared_K p1 .. pn = <body text>` before the first
        enclosing structure item; replace every occurrence's source span
        by `(dedup_shared_K p1 .. pn)`, where p1..pn are the free
        variables not resolvable to earlier top-level bindings.
        All edits are textual splices => the rest of the file (comments,
        formatting) survives byte-for-byte.
     6. Re-parse, repeat until no class above threshold or the iteration
        cap is hit.  Write the result + a fold log on stdout. *)

open Parsetree
open Longident

(* ---------------- CLI ---------------- *)

let threshold = ref 25
let max_iters = ref 10
let same_ctx_ok = ref false
let dry_run = ref false
let file = ref ""

let usage =
  "usage: dedup <file.ml> [--threshold N] [--max-iters N] \
   [--same-context-ok] [--dry-run]"

(* ---------------- small helpers ---------------- *)

let read_file path =
  let ic = open_in_bin path in
  let len = in_channel_length ic in
  let s = really_input_string ic len in
  close_in ic; s

let count_newlines s =
  let n = ref 0 in String.iter (fun c -> if c = '\n' then incr n) s; !n

(* naive substring test *)
let contains s sub =
  let ls = String.length s and lsub = String.length sub in
  let rec go i =
    i + lsub <= ls && (String.sub s i lsub = sub || go (i + 1))
  in
  lsub <= ls && go 0

(* valid value identifier (lowercase start; excludes operator names) *)
let is_var_name s =
  String.length s > 0 && s <> "_" &&
  (match s.[0] with 'a' .. 'z' | '_' -> true | _ -> false)

(* ---------------- AST helpers ---------------- *)

(* strip all locations so structural comparison is location-independent *)
let strip_mapper : Ast_mapper.mapper =
  { Ast_mapper.default_mapper with
    Ast_mapper.location = (fun _ _ -> Location.none) }

let key_of (e : expression) : string =
  Marshal.to_string (strip_mapper.Ast_mapper.expr strip_mapper e) []

(* number of expression nodes in a subtree (self included) *)
let node_count (e : expression) : int =
  let n = ref 0 in
  let it =
    { Ast_iterator.default_iterator with
      Ast_iterator.expr =
        (fun it e -> incr n; Ast_iterator.default_iterator.expr it e) }
  in
  it.Ast_iterator.expr it e; !n

(* free variables of a subtree: unqualified identifiers used minus names
   bound by patterns inside the subtree; first-use order preserved.
   Operator names and constructor-ish names are excluded upfront. *)
let free_vars (e : expression) : string list =
  let used = ref [] in
  let bound = ref [] in
  let it =
    { Ast_iterator.default_iterator with
      Ast_iterator.expr =
        (fun it e ->
          (match e.pexp_desc with
           | Pexp_ident { txt = Lident s; _ }
             when is_var_name s && not (List.mem s !used) ->
               used := !used @ [ s ]
           | _ -> ());
          Ast_iterator.default_iterator.expr it e);
      Ast_iterator.pat =
        (fun it p ->
          (match p.ppat_desc with
           | Ppat_var { txt = s; _ }
           | Ppat_alias (_, { txt = s; _ }) ->
               bound := s :: !bound
           | _ -> ());
          Ast_iterator.default_iterator.pat it p) }
  in
  it.Ast_iterator.expr it e;
  List.filter (fun s -> not (List.mem s !bound)) !used

(* names bound by the patterns of one top-level value item *)
let pattern_vars (p : pattern) : string list =
  let acc = ref [] in
  let it =
    { Ast_iterator.default_iterator with
      Ast_iterator.pat =
        (fun it p ->
          (match p.ppat_desc with
           | Ppat_var { txt = s; _ } -> acc := s :: !acc
           | _ -> ());
          Ast_iterator.default_iterator.pat it p) }
  in
  it.Ast_iterator.pat it p; !acc

(* names bound by top-level items with index < [idx] *)
let globals_before (s : structure) (idx : int) : string list =
  List.concat
    (List.mapi
       (fun i item ->
         if i >= idx then []
         else
           match item.pstr_desc with
           | Pstr_value (_, vbs) -> List.concat_map (fun vb -> pattern_vars vb.pvb_pat) vbs
           | Pstr_primitive vd -> [ vd.pval_name.txt ]
           | _ -> [])
       s)

(* first variable bound by a binding pattern (context name) *)
let first_var_name (p : pattern) : string option =
  match pattern_vars p with s :: _ -> Some s | [] -> None

(* ---------------- occurrence enumeration ---------------- *)

type occ = {
  oexp : expression;
  n : int; (* expression-node count *)
  off0 : int;
  off1 : int;
  ctx : string;
  item_idx : int;
  item_start : int;
}

let is_trivial (e : expression) : bool =
  match e.pexp_desc with
  | Pexp_constant _ | Pexp_ident _ | Pexp_construct (_, None) -> true
  | _ -> false

let collect (s : structure) : occ list =
  let ctx = ref "" in
  let item_idx = ref (-1) in
  let item_start = ref 0 in
  let depth = ref 0 in
  let acc = ref [] in
  let push (e : expression) =
    (* skip ghost nodes (parser desugar artifacts like the synthetic
       `fun` under `let f x = ...`): their spans don't cover a valid
       expression, so textual splicing of them would break the file *)
    if (not (is_trivial e)) && not e.pexp_loc.Location.loc_ghost then begin
      let off0 = e.pexp_loc.loc_start.Lexing.pos_cnum in
      let off1 = e.pexp_loc.loc_end.Lexing.pos_cnum in
      let n = node_count e in
      if off1 > off0 && n >= !threshold then
        acc := { oexp = e; n; off0; off1; ctx = !ctx;
                 item_idx = !item_idx; item_start = !item_start } :: !acc
    end
  in
  let it =
    { Ast_iterator.default_iterator with
      Ast_iterator.expr =
        (fun it e -> push e; Ast_iterator.default_iterator.expr it e);
      Ast_iterator.structure =
        (fun it s ->
          incr depth;
          if !depth = 1 then
            List.iteri
              (fun i item ->
                item_idx := i;
                item_start := item.pstr_loc.loc_start.Lexing.pos_cnum;
                (match item.pstr_desc with
                 | Pstr_value (_, vbs) ->
                     List.iter
                       (fun vb ->
                         ctx :=
                           (match first_var_name vb.pvb_pat with
                            | Some nm -> nm
                            | None -> Printf.sprintf "#item%d" i);
                         it.Ast_iterator.value_binding it vb)
                       vbs
                 | _ ->
                     ctx := Printf.sprintf "#item%d" i;
                     it.Ast_iterator.structure_item it item))
              s
          else Ast_iterator.default_iterator.structure it s;
          decr depth) }
  in
  it.Ast_iterator.structure it s; List.rev !acc

(* ---------------- clone classes ---------------- *)

type clone_class = {
  score : int;
  nodes : int;
  occurrences : occ list; (* after containment pruning *)
}

(* drop occurrences whose span is strictly contained inside a same-class
   span (keep the outer one) *)
let prune_nested (os : occ list) : occ list =
  let by_size = List.sort (fun a b -> compare (b.off1 - b.off0) (a.off1 - a.off0)) os in
  List.fold_left
    (fun kept o ->
      let nested =
        List.exists
          (fun k ->
            k.off0 <= o.off0 && o.off1 <= k.off1
            && (k.off0 <> o.off0 || k.off1 <> o.off1))
          kept
      in
      if nested then kept else kept @ [ o ])
    [] by_size

let find_best_class (excluded : string list) (s : structure)
  : (string * clone_class) option =
  let tbl : (string, occ list) Hashtbl.t = Hashtbl.create 127 in
  List.iter
    (fun o ->
      let k = key_of o.oexp in
      Hashtbl.replace tbl k (o :: (try Hashtbl.find tbl k with Not_found -> [])))
    (collect s);
  let best = ref None in
  Hashtbl.iter
    (fun key os ->
      if (not (List.mem key excluded)) && List.length os >= 2 then begin
        let kept = prune_nested os in
        if List.length kept >= 2 then begin
          let ctxs = List.sort_uniq compare (List.map (fun o -> o.ctx) kept) in
          if !same_ctx_ok || List.length ctxs >= 2 then begin
            let repr = List.hd kept in
            let cls =
              { score = repr.n * List.length kept; nodes = repr.n; occurrences = kept }
            in
            match !best with
            | Some (_, b) when b.score >= cls.score -> ()
            | _ -> best := Some (key, cls)
          end
        end
      end)
    tbl;
  !best

(* ---------------- folding (textual splice) ---------------- *)

let splice (text : string) (edits : (int * int * string) list) : string =
  let edits = List.sort (fun (a, _, _) (b, _, _) -> compare b a) edits in
  List.fold_left
    (fun t (a, b, rep) ->
      String.sub t 0 a ^ rep ^ String.sub t b (String.length t - b))
    text edits

(* fold one clone class; returns (new text, log line, lines saved) or None *)
let fold_class (s : structure) (text : string) (ctr : int ref)
    (cls : clone_class) : (string * string * int) option =
  let occs = List.sort (fun a b -> compare a.off0 b.off0) cls.occurrences in
  match occs with
  | [] -> None
  | repr :: _ ->
      let body = String.sub text repr.off0 (repr.off1 - repr.off0) in
      let free = free_vars repr.oexp in
      let globals = globals_before s repr.item_idx in
      let params = List.filter (fun v -> not (List.mem v globals)) free in
      (* fresh function name, not already present in the file text *)
      let name =
        let rec fresh () =
          incr ctr;
          let cand = Printf.sprintf "dedup_shared_%d" !ctr in
          if contains text cand then fresh () else cand
        in
        fresh ()
      in
      let call =
        match params with
        | [] -> Printf.sprintf "(%s)" name
        | _ -> Printf.sprintf "(%s %s)" name (String.concat " " params)
      in
      let newlet =
        Printf.sprintf "let %s%s =\n%s\n\n" name
          (match params with
           | [] -> ""
           | _ -> " " ^ String.concat " " params)
          body
      in
      let edits =
        (repr.item_start, repr.item_start, newlet)
        :: List.map (fun (o : occ) -> (o.off0, o.off1, call)) occs
      in
      let text' = splice text edits in
      let saved = count_newlines text - count_newlines text' in
      let ctxs = List.sort_uniq compare (List.map (fun o -> o.ctx) occs) in
      Some
        ( text',
          Printf.sprintf
            "FOLD %s nodes=%d occurrences=%d contexts=[%s] params=[%s] \
             lines-saved=%d"
            name cls.nodes (List.length occs) (String.concat "; " ctxs)
            (String.concat " " params) saved,
          saved )

(* ---------------- driver over iterations ---------------- *)

let parse (text : string) : structure option =
  try Some (Parse.implementation (Lexing.from_string text))
  with _ -> None

let () =
  Arg.parse
    [ ("--threshold", Arg.Set_int threshold, " minimum expression-node count of a candidate (default 25)");
      ("--max-iters", Arg.Set_int max_iters, " fixpoint iteration cap (default 10)");
      ("--same-context-ok", Arg.Set same_ctx_ok,
       " allow clone classes whose occurrences live in a single top-level binding");
      ("--dry-run", Arg.Set dry_run, " analyze and log, but do not rewrite the file") ]
    (fun f ->
      if !file = "" then file := f else (Printf.eprintf "%s\n" usage; exit 1))
    usage;
  if !file = "" then begin Printf.eprintf "%s\n" usage; exit 1 end;

  let text0 = read_file !file in
  let lines0 = String.length text0 in
  Printf.printf "FILE %s bytes=%d lines=%d threshold=%d\n%!" !file lines0
    (count_newlines text0) !threshold;

  let name_ctr = ref 0 in
  let folds = ref 0 in
  (* class keys permanently skipped for this file (unproductive folds) *)
  let skipped_keys : string list ref = ref [] in
  let final_text =
    let rec loop text iter_no =
      if iter_no > !max_iters then begin
        Printf.printf "STOP max-iterations=%d reached\n%!" !max_iters; text
      end
      else
        match parse text with
        | None ->
            Printf.printf "STOP re-parse failed; keeping iteration %d text\n%!"
              (iter_no - 1);
            text
        | Some s ->
            (* try classes best-first, skipping ones whose spliced text
               fails to re-parse or that would make the file longer *)
            let rec try_classes excluded =
              match find_best_class (!skipped_keys @ excluded) s with
              | None -> None
              | Some (key, cls) ->
                  (match fold_class s text name_ctr cls with
                   | Some (text', logline, saved) ->
                       (match parse text' with
                        | Some _ when saved >= 0 -> Some (text', logline)
                        | Some _ ->
                            Printf.printf
                              "SKIP nodes=%d occurrences=%d reason=%s\n%!"
                              cls.nodes
                              (List.length cls.occurrences)
                              "would grow the file (negative line savings)";
                            skipped_keys := key :: !skipped_keys;
                            try_classes excluded
                        | None ->
                            Printf.printf
                              "SKIP nodes=%d occurrences=%d reason=%s\n%!"
                              cls.nodes
                              (List.length cls.occurrences)
                              "splice re-parse failed";
                            skipped_keys := key :: !skipped_keys;
                            try_classes excluded)
                   | None -> try_classes (key :: excluded))
            in
            (match try_classes [] with
             | None ->
                 Printf.printf "DONE no-clone-class-above-threshold folds=%d\n%!"
                   !folds;
                 text
             | Some (text', logline) ->
                 incr folds;
                 Printf.printf "%s\n%!" logline;
                 loop text' (iter_no + 1))
    in
    loop text0 1
  in

  Printf.printf
    "RESULT folds=%d bytes-before=%d bytes-after=%d lines-before=%d \
     lines-after=%d\n"
    !folds lines0 (String.length final_text) (count_newlines text0)
    (count_newlines final_text);

  if Sys.getenv_opt "DEDUP_DEBUG" = Some "1" then
    Printf.printf "---FINAL-TEXT---\n%s\n---END---\n" final_text;
  if (not !dry_run) && !folds > 0 then begin
    let oc = open_out_bin !file in
    output_string oc final_text; close_out oc;
    Printf.printf "WROTE %s\n" !file
  end
