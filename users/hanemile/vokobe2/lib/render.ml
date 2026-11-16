open Sexp

(* cumulative profiling, printed by bin/main.ml when VOKOBE_PROFILE=1.
   The ref bumps below are intentionally NOT locked: page rendering is
   parallelized over domains and racing += loses some increments, i.e.
   the reported totals/n are a racy lower bound.  Acceptable -- they are
   diagnostics, not results. *)
let prof_time = Util.prof_time

let prof_query_t = ref 0.
let prof_query_n = ref 0
let prof_sxparse_t = ref 0.
let prof_backlinks_t = ref 0.
let prof_navbar_t = ref 0.

(* Mutable per-render heading bookkeeping: numbering counters, TOC index
   and the headings emitted dynamically by (query ... (group tag) ...).
   This is the single source of truth for heading numbering; a fresh
   instance is created for each render pass and threaded through the
   context so the query renderer (which never sees render_state) can
   register its headings too. *)
type heading_state = {
  mutable counters : int list;
  mutable toc_index : int;
  mutable dynamic_headings : Heading.heading list;
}

let new_heading_state () : heading_state =
  { counters = []; toc_index = 0; dynamic_headings = [] }

type page_index = (string, Page.page) Hashtbl.t

(* name -> page, built once per site and shared by all render passes
   (replaces O(n) List.find_opt scans in the render hot path)
   Complexity: O(n); n = pages. *)
let page_index_of (pages : Page.page list) : page_index =
  let t = Hashtbl.create (List.length pages * 2) in
  List.iter (fun (p : Page.page) -> Hashtbl.replace t p.Page.name p) pages;
  t

type context = {
  pages : Page.page list;
  page_index : page_index;
  current : Page.page;
  headings : Heading.heading list;
  hstate : heading_state;
  map_cache_dir : string;
  (* first pass only: skip output-only work (maps, nav, backlinks, toc);
     (query ...) still runs so its dynamic headings are collected *)
  collect_only : bool;
  (* set when the page explicitly invokes (backlinks); render_html then
     skips the automatic backlink box to avoid a duplicate *)
  mutable backlinks_op_used : bool;
  (* book-format chapter nesting depth; > 0 disables nested chapter
     rendering so a book page can never include itself (directly or
     transitively) *)
  book_depth : int;
  (* book-format chapter rendering: dimg ops emitted so far in the current
     chapter and the chapter's total.  Past book_image_max, gallery
     images collapse into one link to the standalone page.  Fresh values
     per chapter; irrelevant outside books. *)
  book_image_count : int ref;
  book_image_total : int;
  (* heading-level shift for chapter content inside book-format pages:
     0 on a
     normal page, 1 inside a chapter so its content nests below the
     chapter title *)
  heading_offset : int;
  (* feed filename + tag for pages carrying their own per-tag rss feed
     (tag pages); other pages advertise only the blog feed *)
  feed : (string * string) option;
}

let page_link (p : Page.page) : string = "/" ^ p.name

(* first value of an attribute, if present and non-empty
   Complexity: O(a); a = attrs of the page (assoc-list scan). *)
let first_attr (p : Page.page) (key : string) : string option =
  match List.assoc_opt key p.Page.attrs with
  | Some (v :: _) -> Some v
  | _ -> None

(* a page's tags, [] when untagged
   Complexity: O(a); a = attrs of the page. *)
let tags_of (p : Page.page) : string list =
  Option.value ~default:[] (List.assoc_opt "tag" p.Page.attrs)

(* string compare honoring a desc direction flag
   Complexity: O(len of shorter string). *)
let cmp_str ~desc a b =
  let c = String.compare a b in
  if desc then -c else c

(* Group items by key, merging non-adjacent occurrences of the same key.
   Keys keep first-appearance order, items keep their incoming order.
   Complexity: O(i); i = items (Hashtbl lookup per item, rev-append into
   the bucket, one List.rev per group at the end). *)
let group_by_key (key_of : 'a -> string) (items : 'a list) : (string * 'a list) list =
  let groups : (string, 'a list) Hashtbl.t = Hashtbl.create 16 in
  let keys = ref [] in
  List.iter
    (fun x ->
      let k = key_of x in
      match Hashtbl.find_opt groups k with
      | Some xs -> Hashtbl.replace groups k (x :: xs)
      | None ->
          Hashtbl.replace groups k [x];
          keys := k :: !keys)
    items;
  List.map (fun k -> (k, List.rev (Hashtbl.find groups k))) (List.rev !keys)

(* O(1): Hashtbl lookup. *)
let tag_link (index : page_index) (tag : string) : string =
  match Hashtbl.find_opt index tag with
  | Some target_page -> "/" ^ target_page.name
  | None -> "/" ^ tag

(* Date values are rendered as links into the generated date archive
   (/YYYY, /YYYY-MM, /YYYY-MM-DD).  Only link the parts that actually have
   a page -- e.g. a (modified ...) stamp or a (published ...) date that
   never became a date key must fall back to plain text.
   Complexity: O(len date_str): a constant number of Hashtbl lookups. *)
let format_date_links (index : page_index) (date_str : string) : string =
  let part name label =
    if Hashtbl.mem index name then
      Printf.sprintf {|<a href="/%s" class="local">%s</a>|} name
        (Util.html_escape label)
    else Util.html_escape label
  in
  match String.split_on_char '-' (String.trim date_str) with
  | [year; month; day] ->
      Printf.sprintf "%s-%s-%s" (part year year)
        (part (year ^ "-" ^ month) month)
        (part (year ^ "-" ^ month ^ "-" ^ day) day)
  | [year; month] ->
      Printf.sprintf "%s-%s" (part year year) (part (year ^ "-" ^ month) month)
  | [year] -> part year year
  | _ -> Util.html_escape date_str

(* a value naming a page renders as a local link; anything else as
   plain escaped text
   Complexity: O(len v) for sanitize/escape; the page lookup is O(1). *)
let local_link (ctx : context) (v : string) : string =
  let slug = Util.sanitize v in
  match Hashtbl.find_opt ctx.page_index slug with
  | Some _ ->
      Printf.sprintf {|<a href="/%s" class="local">%s</a>|} slug
        (Util.html_escape v)
  | None -> Util.html_escape v

(* Rendered/HTML value of an attribute, for display (info bar, table
   columns): dates become links, values naming a page become links.
   Complexity: O(a) assoc scans, a = page attrs; the location branch is
   O(l) in l = number of cities (cities and countries zipped once). *)
let get_page_attr (ctx : context) (p : Page.page) (key : string) : string =
  (* single ISO date rendered as archive links *)
  let links d = format_date_links ctx.page_index d in
  (* "start" or "start – end", the end only when it differs *)
  let range start =
    match start with
    | Some d ->
        let start_html = links d in
        (match first_attr p "date-end" with
         | Some e when e <> d -> start_html ^ " – " ^ links e
         | _ -> start_html)
    | None -> ""
  in
  let single key = match first_attr p key with Some d -> links d | None -> "" in
  match key with
  | "title" -> p.title
  | "name" -> p.name
  | "link" -> page_link p
  | "date-range" -> range (first_attr p "date-start")
  | "published" | "modified" -> single key
  | "date" ->
      range
        (match first_attr p "date-start" with
         | None -> (
             match first_attr p "published" with
             | None -> first_attr p "date"
             | found -> found)
         | found -> found)
  | "date-end" -> single "date-end"
  | "tag" | "tags" ->
      tags_of p
      |> List.map (fun t ->
             Printf.sprintf
             {|<a href="%s" class="local">%s</a>|}
             (tag_link ctx.page_index t) (Util.html_escape t))
      |> String.concat ", "
  | "location" ->
      (* Zip cities and countries into "City, Country" pairs, " / " separated *)
      let cities    = Option.value ~default:[] (List.assoc_opt "location" p.attrs) in
      let countries = Option.value ~default:[] (List.assoc_opt "country"  p.attrs) in
      let rec zip cities countries =
        match cities with
        | [] -> []
        | city :: cs ->
            let city_html = local_link ctx city in
            (match countries with
             | c :: cs' -> (city_html ^ ", " ^ local_link ctx c) :: zip cs cs'
             | [] -> city_html :: zip cs [])
      in
      String.concat " / " (zip cities countries)
  | _ -> (
      match List.assoc_opt key p.attrs with
      | Some vals -> vals |> List.map (local_link ctx) |> String.concat ", "
      | None -> "")

(* Raw (unformatted) value of an attribute, for query filters/sorts:
   attrs lookup, first value; the virtual keys (title/name/link) and the
   "date" fallback chain mirror get_page_attr, but no HTML is produced.
   ISO dates compare correctly as plain strings.
   Complexity: O(a); a = attrs of the page. *)
let get_page_attr_raw (p : Page.page) (key : string) : string =
  let lookup k = Option.value ~default:"" (first_attr p k) in
  match key with
  | "title" -> p.title
  | "name" -> p.name
  | "link" -> p.name
  | "date" ->
      (* first non-empty of the fallback chain *)
      [ "date-start"; "published"; "date" ]
      |> List.filter_map (first_attr p)
      |> List.find_opt (fun v -> v <> "")
      |> Option.value ~default:""
  | "tag" | "tags" -> (
      match List.assoc_opt "tag" p.attrs with
      | Some tags -> String.concat " " tags
      | None -> "")
  | "location" -> (
      match List.assoc_opt "location" p.attrs with
      | Some cities -> String.concat " " cities
      | None -> "")
  | _ -> lookup key

(* O(k * a): get_page_attr per info key; k = info keys, a = page attrs. *)
let render_info_bar (ctx : context) : string =
  let p = ctx.current in
  match List.assoc_opt "info" p.attrs with
  | None | Some [] -> ""
  | Some keys ->
      let items =
        List.filter_map
          (fun key ->
            let val_str = get_page_attr ctx p key in
            if val_str = "" then None
            else
              let index_key = match key with
                | "published" | "modified" | "date-range" -> "date"
                | k -> k
              in
              let key_label =
                match Hashtbl.find_opt ctx.page_index index_key with
                | Some _ ->
                    Printf.sprintf
                    {|<a href="/%s" class="local">%s</a>|}
                    index_key (Util.html_escape key)
                | None -> Util.html_escape key
              in
              Some
                (Printf.sprintf
                {|<span class="info-item info-%s"><strong>%s:</strong> %s</span>|}
                key key_label val_str))
          keys
      in
      if items = [] then ""
      else
        "<div class=\"info-bar\">\n  " ^
        String.concat " <br> " items ^
        "\n</div>\n"

(* O(t * a) per page: t = nodes in the filter sexp, each leaf an O(a)
   attr lookup; a = attrs of the page. *)
let rec eval_filter (p : Page.page) (sx : sexp) : bool =
  match sx with
  | Atom "true" -> true
  | List [Atom "tag"; Atom t] -> List.mem t (tags_of p)
  | List [Atom "eq"; Atom key; Atom target] ->
      get_page_attr_raw p key = target
  | List [Atom "contains"; Atom key; Atom needle] ->
      Util.contains_substring (get_page_attr_raw p key) needle
  | List [Atom "has"; Atom key; Atom needle] ->
      (match List.assoc_opt key p.attrs with
       | Some vals -> List.mem needle vals
       | None -> false)
  | List [Atom "has-photos"] ->
      Util.contains_substring p.Page.raw "(dimg "
  | List (Atom "and" :: rest) ->
      List.for_all (eval_filter p) rest
  | List (Atom "or" :: rest) ->
      List.exists (eval_filter p) rest
  | List [Atom "not"; sub] ->
      not (eval_filter p sub)
  | _ -> false

(* Cross-page query memoization.  Most (query ...) sexps -- navbar,
   index tables -- are textually identical on every page yet were
   re-evaluated per page and per render pass.  The cache is keyed on the
   canonical sexp string (Roughmap.sexp_key) and lives in a slot that,
   like backlinks_cache, is keyed on the physical identity of the pages
   list.  Two kinds of queries bypass the cache:
   - any query containing a (group ...) directive: group queries emit
     dynamic headings into ctx.hstate and MUST execute on every pass;
   - queries whose result set contains the current page itself: the
     list format marks it class="active", so the html is page-specific.
     For all pages not in a query's result set its html is identical,
     so the cache entry records the matched page names and a lookup by
     a listed page recomputes (and is not stored back). *)
type query_cache_entry = { qc_html : string; qc_names : string list }

let query_cache :
    (Page.page list * (string, query_cache_entry) Hashtbl.t) option ref =
  ref None

(* parallel page rendering shares the table: serialize probe+replace *)
let query_cache_lock = Mutex.create ()

let with_query_cache_lock f =
  Mutex.lock query_cache_lock;
  Fun.protect f ~finally:(fun () -> Mutex.unlock query_cache_lock)

let query_cache_table (pages : Page.page list) =
  match !query_cache with
  | Some (key, tbl) when key == pages -> tbl
  | Some _ | None ->
      let tbl = Hashtbl.create 64 in
      query_cache := Some (pages, tbl);
      tbl

(* The query engine.  Complexity over n = pages with a = attrs/page:
   filtering O(n * a), sorting O(n log n) comparisons with an O(a) raw
   attr lookup each, each emitted row O(cols * a).  Grouping adds:
   group_by_key/field O(n) (see group_by_key),
   group-by-tag O(t * n) for t = tag sections (one filter pass per tag)
   plus O(t) dynamic-heading appends.  Output string built in one concat. *)
(* ---------------------------------------------------------------------------
   card grids ((query ... (format gallery)), backlink galleries): one card
   per page - first image of the page (the pre-generated undithered
   -card.webp thumbnail when it comes from a dimg op), linked title,
   short description line. *)

let find_sub_opt (hay : string) (needle : string) : int option =
  let hn = String.length hay and nn = String.length needle in
  let rec loop i =
    if i + nn > hn then None
    else if String.sub hay i nn = needle then Some i
    else loop (i + 1)
  in
  loop 0

(* first image referenced in a page body: (dimg ...) or (img ...) op or
   markdown ![alt](url).  For dimg the first url (the dithered preview)
   wins; render_card swaps it for the pre-generated undithered
   <stem>-card.webp thumbnail (max 200 wide, see publish-photos.py). *)
let first_image_url (raw : string) : string option =
  let atom_after i =
    let j = ref i in
    while !j < String.length raw && raw.[!j] <= ' ' do incr j done;
    let j0 = !j in
    while !j < String.length raw
          && not (String.contains " \t\n\r()\"" raw.[!j])
    do incr j done;
    if !j > j0 then Some (String.sub raw j0 (!j - j0)) else None
  in
  let via_op op =
    match find_sub_opt raw ("(" ^ op ^ " ") with
    | Some i -> atom_after (i + String.length op + 2)
    | None -> None
  in
  match via_op "dimg" with
  | Some _ as u -> u
  | None -> (match via_op "img" with
             | Some _ as u -> u
             | None ->
               (* markdown: ![alt](url) - the '!' matters, plain links
                  [t](url) are not images *)
               (match find_sub_opt raw "![" with
                | Some bang ->
                    (match find_sub_opt (String.sub raw bang (String.length raw - bang)) "](" with
                     | Some k ->
                         let open_ = bang + k + 2 in
                         let j = try String.index_from raw open_ ')'
                                 with Not_found -> open_ in
                         if j > open_ then Some (String.sub raw open_ (j - open_))
                         else None
                     | None -> None)
                | None -> None))

let card_desc (p : Page.page) : string =
  match first_attr p "description" with
  | Some d -> d
  | None -> (match List.assoc_opt "date" p.Page.attrs with
             | Some (a :: b :: _) -> a ^ " – " ^ b
             | Some [ a ] -> a
             | _ -> Option.value ~default:"" (first_attr p "published"))

(* the undithered ~200px preview of a dimg photo ('<stem>-card.webp'),
   generated by publish-photos.py; any other url passes through *)
let card_preview_url (url : string) : string =
  if String.ends_with ~suffix:"-dither.webp" url then
    String.sub url 0 (String.length url - String.length "-dither.webp")
    ^ "-card.webp"
  else url

let render_card (ctx : context) (p : Page.page) (href : string) : string =
  let img_html =
    match first_image_url p.Page.raw with
    | Some url ->
        let url = card_preview_url url in
        Printf.sprintf
          "<div class=\"cardimg\" style=\"background-image:url(%s)\"></div>"
          (Util.html_escape url)
    | None ->
        (* surfaced in the build log: galleries with image-less cards *)
        Printf.eprintf
          "[!] gallery card without image: '%s' (linked from /%s)\n%!"
          p.title ctx.current.name;
        ""
  in
  Printf.sprintf
    "<a class=\"card\" href=\"%s\">%s<div class=\"cardtitle\">%s</div>\
     <div class=\"carddesc\">%s</div></a>\n"
    (Util.html_escape href) img_html
    (Util.html_escape p.title) (Util.html_escape (card_desc p))

let cardgrid (cards : string list) : string =
  "<div class=\"cardgrid\">\n" ^ String.concat "" cards ^ "</div>"

(* [full-res: n MB] links for dimg photos are emitted at build time from
   published-photos.json (stem -> orig filename/bytes/EXIF exposure info)
   instead of being pasted into the markdown.  Immich annotations synced
   into the manifest by publish-photos.py ('tags'/'desc') render on the
   same info line.  Loaded lazily once per process; a missing or
   malformed manifest just yields no links.  Rendering is parallel over
   domains, so the one-time load is mutex-guarded. *)
type fullres_entry = {
  fr_orig : string;
  fr_bytes : float;
  fr_shutter : string option;  (* "1/250" or "2.5" (seconds) *)
  fr_iso : int option;
  fr_aperture : string option; (* "f/2.8" *)
  fr_focal : string option;    (* "35" (mm, physical) *)
  fr_focal_eq : string option; (* "50" (mm, 35mm equivalent) *)
  fr_dt : string option;       (* "2024-11-11T12:00:00" (ISO 8601) *)
  fr_tags : string list;       (* Immich tag values, e.g. "trip/asia" *)
  fr_desc : string option;     (* Immich description *)
}

let fullres_lock = Mutex.create ()

let fullres_cache : (string, fullres_entry) Hashtbl.t option ref =
  ref None

let load_fullres () : (string, fullres_entry) Hashtbl.t =
  let t = Hashtbl.create 1024 in
  let path =
    match Sys.getenv_opt "VOKOBE_PHOTOS_MANIFEST" with
    | Some p -> p
    | None -> "published-photos.json"
  in
  (try
     match Json.parse (Util.read_file path) with
     | Json.Obj entries ->
         List.iter
           (fun (_, e) ->
             match Json.get_string "main" e with
             | Some main -> (
                 let stem =
                   let base =
                     match String.rindex_opt main '/' with
                     | Some i -> String.sub main (i + 1)
                                   (String.length main - i - 1)
                     | None -> main
                   in
                   match String.rindex_opt base '-' with
                   | Some i when String.length base - i > 2 ->
                       (* '<stem>-1600w.webp' -> '<stem>' *)
                       (try
                          let _ = int_of_string
                            (String.sub base (i + 1)
                               (String.length base - i - 7)) in
                          String.sub base 0 i
                        with _ -> base)
                   | _ -> base
                 in
                 match
                   (Json.get_string "orig" e, Json.get_float "orig_bytes" e)
                 with
                 | Some orig, Some bytes ->
                     let exif_str key =
                       match Json.member "exif" e with
                       | Some o -> Json.get_string key o
                       | None -> None
                     in
                     let iso =
                       match Json.member "exif" e with
                       | Some o -> (
                           match Json.member "iso" o with
                           | Some (Json.Num n) -> Some (int_of_float n)
                           | Some (Json.Str s) -> int_of_string_opt s
                           | _ -> None)
                       | None -> None
                     in
                     let tags =
                       match Json.member "tags" e with
                       | Some (Json.Arr xs) ->
                           List.filter_map
                             (function Json.Str s -> Some s | _ -> None)
                             xs
                       | _ -> []
                     in
                     Hashtbl.replace t stem
                       {
                         fr_orig = orig;
                         fr_bytes = bytes;
                         fr_shutter = exif_str "shutter";
                         fr_iso = iso;
                         fr_aperture = exif_str "f";
                         fr_focal = exif_str "mm";
                         fr_focal_eq = exif_str "eq";
                         fr_dt = exif_str "dt";
                         fr_tags = tags;
                         fr_desc = Json.get_string "desc" e;
                       }
                 | _ -> ())
             | None -> ())
           entries
     | _ -> ()
   with _ -> ());
  t

let fullres_index () : (string, fullres_entry) Hashtbl.t =
  Mutex.lock fullres_lock;
  Fun.protect
    ~finally:(fun () -> Mutex.unlock fullres_lock)
    (fun () ->
      match !fullres_cache with
      | Some t -> t
      | None ->
          let t = load_fullres () in
          fullres_cache := Some t;
          t)

(* stem of the image a dimg dither url points at:
   'https://img.emile.space/<stem>-dither.webp' -> Some '<stem>' *)
let dither_stem (url : string) : string option =
  if String.ends_with ~suffix:"-dither.webp" url then
    let base =
      match String.rindex_opt url '/' with
      | Some i ->
          String.sub url (i + 1) (String.length url - i - 1)
      | None -> url
    in
    Some
      (String.sub base 0
         (String.length base - String.length "-dither.webp"))
  else None

(* immich photo descriptions are free text; support the markdown link
   form [text](https://...) (description authors naturally paste wiki
   links) - everything else html-escaped, only http(s) targets linked *)
let linkify_desc (s : string) : string =
  let n = String.length s in
  let buf = Buffer.create (n + 32) in
  let esc = Util.html_escape in
  let find_from ch i =
    let rec go k =
      if k >= n then None
      else if s.[k] = ch then Some k
      else go (k + 1)
    in
    go i
  in
  let is_url u =
    String.starts_with ~prefix:"http://" u
    || String.starts_with ~prefix:"https://" u
  in
  let rec loop i =
    if i < n then
      match find_from '[' i with
      | None -> Buffer.add_string buf (esc (String.sub s i (n - i)))
      | Some lb -> (
          Buffer.add_string buf (esc (String.sub s i (lb - i)));
          match find_from ']' lb with
          | Some rb when rb + 1 < n && s.[rb + 1] = '(' -> (
              (* urls may contain parens (wikipedia!): close at the ')'
                 that balances out any '(' inside the url *)
              let rec find_close k depth =
                if k >= n then None
                else
                  match s.[k] with
                  | '(' -> find_close (k + 1) (depth + 1)
                  | ')' ->
                      if depth = 0 then Some k
                      else find_close (k + 1) (depth - 1)
                  | _ -> find_close (k + 1) depth
              in
              match find_close (rb + 2) 0 with
              | Some rp ->
                  let text = String.sub s (lb + 1) (rb - lb - 1) in
                  let url = String.sub s (rb + 2) (rp - rb - 2) in
                  if is_url url then (
                    Buffer.add_string buf
                      (Printf.sprintf "<a href=\"%s\">%s</a>" (esc url)
                         (esc text));
                    loop (rp + 1))
                  else (
                    Buffer.add_string buf
                      (esc (String.sub s lb (rp - lb + 1)));
                    loop (rp + 1))
              | None -> Buffer.add_string buf (esc (String.sub s lb (n - lb))))
          | _ ->
              Buffer.add_string buf (esc (String.sub s lb (n - lb))))
  in
  loop 0;
  Buffer.contents buf

let fullres_link (dither_url : string) : string =
  match dither_stem dither_url with
  | None -> ""
  | Some stem -> (
      match Hashtbl.find_opt (fullres_index ()) stem with
      | Some e ->
          let dir =
            match String.rindex_opt dither_url '/' with
            | Some i -> String.sub dither_url 0 (i + 1)
            | None -> ""
          in
          (* capture date/time + exposure info on the same line:
             " 2024-11-11T12:00:00 | 1/250s | ISO 100 | f/2.8 | 26mm" *)
          let info =
            let parts =
              List.filter_map
                (fun x -> x)
                [
                  e.fr_dt;
                  Option.map (fun s -> s ^ "s") e.fr_shutter;
                  Option.map (Printf.sprintf "ISO %d") e.fr_iso;
                  e.fr_aperture;
                  Option.map
                    (fun s ->
                      (* tiny phone sensors stamp physical focal lengths
                         like 1.54mm: add the 35mm equivalent so the
                         number reads like a lens people know *)
                      match e.fr_focal_eq with
                      | Some eq when eq <> s -> s ^ "mm (" ^ eq ^ "mm)"
                      | _ -> s ^ "mm")
                    e.fr_focal;
                ]
            in
            (* Immich tags after the exposure info:
               "... | 1/250s | ISO 100 | #temple #asia" *)
            let parts =
              match e.fr_tags with
              | [] -> parts
              | ts ->
                  parts
                  @ [ String.concat " " (List.map (fun t -> "#" ^ t) ts) ]
            in
            match parts with
            | [] -> ""
            | _ -> " " ^ String.concat " | " parts
          in
          (* the Immich description gets its own line below;
             [text](https://...) markdown links become anchors *)
          let desc_html =
            match e.fr_desc with
            | Some d when d <> "" ->
                Printf.sprintf "<p class=\"photodesc\">%s</p>"
                  (linkify_desc d)
            | _ -> ""
          in
          Printf.sprintf
            "<p class=\"fullres\"><a href=\"%s%s\">full-res: \
             %.1f MB</a>%s</p>%s"
            (Util.html_escape dir) (Util.html_escape e.fr_orig)
            (e.fr_bytes /. 1e6) (Util.html_escape info) desc_html
      | None -> "")

(* book-format chapter rendering lives far below (it calls back into
   render_body); resolve through this late reference. *)
let render_chapters_late : (context -> Page.page list -> string) ref =
  ref (fun _ _ -> "")

let rec render_query (ctx : context) (args : sexp list) : string =
  incr prof_query_n;
  prof_time prof_query_t @@ fun () ->
  (* queries with any (group ...) clause have heading side effects into
     ctx.hstate (see below): never memoized, executed every pass *)
  let has_group =
    List.exists (function List (Atom "group" :: _) -> true | _ -> false) args
  in
  (* same for the book format: chapter titles are dynamic headings, and
     nested chapters are disabled via ctx.book_depth so a book page can
     never include itself (directly or transitively) *)
  let book_fmt =
    List.exists
      (function List [Atom "format"; Atom "book"] -> true | _ -> false)
      args
  in
  let memo =
    if has_group || book_fmt then None
    else
      let tbl = query_cache_table ctx.pages in
      Some (Roughmap.sexp_key (List args), tbl)
  in
  let hit =
    match memo with
    | Some (key, tbl) -> (
        match with_query_cache_lock (fun () -> Hashtbl.find_opt tbl key) with
        | Some e when not (List.mem ctx.current.name e.qc_names) ->
            Some e.qc_html
        | Some _ | None -> None)
    | None -> None
  in
  match hit with
  | Some html -> html
  | None -> render_query_compute ctx args memo

and render_query_compute (ctx : context) (args : sexp list)
    (memo : (string * (string, query_cache_entry) Hashtbl.t) option) :
    string =
  let where_cond = ref (Atom "true") in
  let sort_key = ref "" in
  let sort_desc = ref false in
  let format_mode = ref "table" in
  let cols = ref [("Title", "title"); ("Date", "date")] in
  let group_by_year = ref "" in
  let group_year_desc = ref false in
  let group_by_field = ref "" in
  let group_field_pair = ref "" in
  let group_by_tag = ref false in
  let group_tag_skip = ref [] in
  let limit_n = ref 0 in

  List.iter
    (function
      | List (Atom "where" :: cond :: _) -> where_cond := cond
      | List [Atom "sort"; Atom key; Atom dir] ->
          sort_key := key;
          sort_desc := (dir = "desc")
      | List [Atom "format"; Atom fmt] -> format_mode := fmt
      | List [Atom "limit"; Atom n] ->
          limit_n := Option.value ~default:0 (int_of_string_opt n)
      | List (Atom "group" :: Atom "year" :: Atom field :: dir) ->
          group_by_year := field;
          group_year_desc := (dir = [Atom "desc"])
      | List (Atom "group" :: Atom "field" :: Atom attr :: pair) ->
          group_by_field := attr;
          group_field_pair := (match pair with [Atom p] -> p | _ -> "")
      (* (group tag) or (group tag "skip-me" ...) to omit sections for given tags *)
      | List (Atom "group" :: Atom "tag" :: skips) ->
          group_by_tag := true;
          group_tag_skip := atoms skips
      | List (Atom "cols" :: raw_cols) ->
          let parsed =
            List.filter_map
              (function
                | Atom name -> Some (String.capitalize_ascii name, name)
                | List [Atom label; Atom field] -> Some (label, field)
                | _ -> None)
              raw_cols
          in
          if parsed <> [] then cols := parsed
      | _ -> ())
    args;

  let matched = List.filter (fun p -> eval_filter p !where_cond) ctx.pages in
  let sorted =
    if !sort_key <> "" then
      List.sort
        (fun a b ->
          cmp_str ~desc:!sort_desc (get_page_attr_raw a !sort_key)
            (get_page_attr_raw b !sort_key))
        matched
    else matched
  in

  let result =
    match !format_mode with
    | "gallery" ->
      let ps =
        if !limit_n > 0 then List.filteri (fun i _ -> i < !limit_n) sorted
        else sorted
      in
      cardgrid (List.map (fun p -> render_card ctx p (page_link p)) ps)

    | "book" ->
        (* reading-mode concatenation: every matched page one chapter,
           chronological; disabled inside chapters (no nesting) *)
        if ctx.book_depth > 0 then ""
        else !render_chapters_late ctx sorted

    | "list" ->
      let items =
        List.map
          (fun p ->
            let is_active = (p.Page.name = ctx.current.name) in
            let class_attr = if is_active then " class=\"active\"" else "" in
            Printf.sprintf
              {|<li><a href="%s"%s>%s</a></li>
|}
              (page_link p) class_attr (Util.html_escape p.title))
          sorted
      in
      "<ul>\n" ^ String.concat "" items ^ "</ul>"

  | _ ->
      let ncols = List.length !cols in
      (* full-width label row separating groups (years, field values) *)
      let group_header label =
        Printf.sprintf {|<tr><td colspan="%d" class="year-group">%s</td></tr>|}
          ncols label
      in
      let header_html =
        "<tr>"
        ^ String.concat ""
            (List.map (fun (label, _) -> Printf.sprintf "<th>%s</th>" (Util.html_escape label)) !cols)
        ^ "</tr>\n"
      in
      let year_of p =
        (* "date" is virtual: same fallback chain as get_page_attr *)
        let lookup key = Option.map String.trim (first_attr p key) in
        let v =
          if !group_by_year = "date" then
            match lookup "date-start" with
            | Some d -> d
            | None -> (match lookup "published" with
                       | Some d -> d
                       | None -> Option.value ~default:"" (lookup "date"))
          else Option.value ~default:"" (lookup !group_by_year)
        in
        match String.split_on_char '-' v with
        | year :: _ -> year
        | [] -> ""
      in
      let make_row p =
        "<tr>"
        ^ String.concat ""
            (List.map
               (fun (_, field) ->
                 let val_str = get_page_attr ctx p field in
                 let cell_content =
                   if field = "title" then
                     Printf.sprintf "<a href=\"%s\">%s</a>" (page_link p) (Util.html_escape p.title)
                   else val_str
                 in
                 Printf.sprintf "<td class=\"col-%s\">%s</td>" field cell_content)
               !cols)
        ^ "</tr>\n"
      in
      let rows_html =
        if !group_by_field <> "" then begin
          let filter_vals p vs =
            if !group_field_pair = "" then vs
            else
              let pairs = Option.value ~default:[]
                (List.assoc_opt !group_field_pair p.Page.attrs) in
              List.filteri (fun i _ ->
                match List.nth_opt pairs i with
                | Some c -> c = ctx.current.title
                | None   -> true)
              vs
          in
          let joined f p =
            match List.assoc_opt !group_by_field p.Page.attrs with
            | Some vs -> String.concat " / " (List.map f (filter_vals p vs))
            | None -> ""
          in
          let grouped = group_by_key (joined String.trim) sorted in
          List.concat_map
            (fun (_, group_pages) ->
              let label = match group_pages with p :: _ -> joined (local_link ctx) p | [] -> "" in
              group_header label :: List.map make_row group_pages)
            grouped
        end
        else if !group_by_year = "" then List.map make_row sorted
        else begin
          (* Collect rows into (year, pages) groups, then sort groups by year *)
          let grouped = group_by_key year_of sorted in
          let grouped_sorted =
            List.sort (fun (a, _) (b, _) -> cmp_str ~desc:!group_year_desc a b) grouped
          in
          List.concat_map
            (fun (yr, pages) ->
              group_header (format_date_links ctx.page_index yr)
              :: List.map make_row pages)
            grouped_sorted
        end
      in
      if !group_by_tag then begin
        let system_tags = ["date"; "city"; "location"; "country"; "tag"; "meta"] in
        let all_tags =
          sorted |> List.concat_map tags_of |> List.sort_uniq String.compare
        in
        let ordered_tags =
          let all_tags = List.filter (fun t -> not (List.mem t !group_tag_skip)) all_tags in
          let sys = List.filter (fun t -> List.mem t all_tags) system_tags in
          let rest = List.filter (fun t -> not (List.mem t system_tags)) all_tags in
          sys @ rest
        in
        let sections = List.filter_map
          (fun tag ->
            let pages_with_tag =
              List.filter (fun p -> List.mem tag (tags_of p)) sorted
            in
            if pages_with_tag = [] then None
            else Some (tag, pages_with_tag))
          ordered_tags
        in
        String.concat "" (List.map
          (fun (tag, pages_with_tag) ->
            (* heading id uses the single heading-slug function; page links
               (local_link) keep the page-name slug *)
            let slug = Heading.slugify tag in
            ctx.hstate.counters <- Heading.update_counters ctx.hstate.counters 2;
            let num_str = Heading.format_counter ctx.hstate.counters in
            let idx = ctx.hstate.toc_index in
            ctx.hstate.toc_index <- idx + 1;
            ctx.hstate.dynamic_headings <- ctx.hstate.dynamic_headings @ [{
              Heading.level = 2; title = tag; slug;
              number_str = num_str ^ " " ^ tag;
            }];
            Printf.sprintf {|<h2 id="%s" style="--is: --h-%d"><a href="#%s">%s %s</a></h2>%!|}
              slug idx slug num_str (local_link ctx tag)
            ^ "<table>\n"
            ^ String.concat "" (List.map make_row pages_with_tag)
            ^ "</table>\n")
          sections)
      end
      else "<table>\n" ^ header_html ^ String.concat "" rows_html ^ "</table>"
  in
  (* store only context-independent html: the list format marks the
     current page class="active", so a page listed by its own query
     result recomputes every time and is not cached *)
  (match memo with
   | Some (key, tbl) ->
       let current_listed =
         !format_mode = "list"
         && List.exists
              (fun (p : Page.page) -> p.Page.name = ctx.current.name)
              sorted
       in
       if not current_listed then
         with_query_cache_lock (fun () ->
             Hashtbl.replace tbl key
               {
                 qc_html = result;
                 qc_names =
                   List.map (fun (p : Page.page) -> p.Page.name) sorted;
               })
   | None -> ());
  result

(* O(n * a): delegates to one tag-filtered query over all n pages. *)
let render_navbar (ctx : context) : string =
  prof_time prof_navbar_t @@ fun () ->
  let nav_args = [
    List [Atom "where"; List [Atom "tag"; Atom "navbar"]];
    List [Atom "format"; Atom "list"];
  ] in
  let links_html = render_query ctx nav_args in
  links_html ^
  {|<ul style="float: right">
    <!-- <li>
      <form method="GET" action="/search">
        <input type="text" id="q" name="q" placeholder="search">
      </form>
    </li>
    <li><a href="README.md">.md</a></li> -->
  </ul>|}

(* O(h); h = headings. *)
let render_toc (headings : Heading.heading list) : string =
  let items =
    List.mapi
      (fun i (h : Heading.heading) ->
        (* --for must be on the <li>: scroll-snap-align only listens to
           block-level boxes, and the li is what gets snapped into view *)
        Printf.sprintf
          {|<li class="toc-h%d" style="--for: --h-%d"><a href="#%s">%s</a></li>|}
          h.level i h.slug h.number_str)
      headings
  in
  "<ul class=\"toc\">\n" ^ String.concat "" items ^ "</ul>"

(* Backlinks index: page name -> referring pages, in page-list order.
   Built once and memoized on the physical identity of the pages list
   (bin/main.ml shares one list across all renders); hidden pages stay
   in the index exactly when they are in the ctx.pages list, as before.
   Complexity: O(n * L) once per pages list (n = pages, L = raw length);
   lookup O(1). *)
(* a backlink: the page that links, plus the anchor of the nearest
   preceding markdown heading (the place on the page it links from) *)
type backlink = { b_page : Page.page; b_anchor : string option }
type backlinks = (string, backlink list) Hashtbl.t

let backlinks_cache : (Page.page list * backlinks) option ref = ref None

(* Only REAL internal link destinations count: markdown [text](/target)
   and (link "/target" ...).  Plain-text "/name" substrings (external
   urls, prose, code) are ignored -- the old any-substring scan produced
   phantom links, e.g. a flickr.com/search url backlinking /search.
   Edges remember the nearest preceding heading so backlinks point at
   the place on the page they link from.  O(n * L). *)
let build_backlinks (pages : Page.page list) : backlinks =
  let names = page_index_of pages in
  let edges : backlinks = Hashtbl.create 256 in
  let add_edge (p : Page.page) seen name anchor =
    if name <> p.Page.name && not (Hashtbl.mem seen name) then begin
      Hashtbl.replace seen name ();
      let prev =
        match Hashtbl.find_opt edges name with Some ps -> ps | None -> []
      in
      Hashtbl.replace edges name
        ({ b_page = p; b_anchor = anchor } :: prev)
    end
  in
  let normalize t =
    (* internal page target: "/name", fragment and trailing slash dropped *)
    let len = String.length t in
    if len < 2 || t.[0] <> '/' || t.[1] = '/' then None
    else
      let t =
        match String.index_opt t '#' with
        | Some i -> String.sub t 0 i
        | None -> t
      in
      let len = String.length t in
      let t =
        if len > 1 && t.[len - 1] = '/' then String.sub t 0 (len - 1)
        else t
      in
      let name = String.sub t 1 (String.length t - 1) in
      if Hashtbl.mem names name then Some name else None
  in
  let scan_line p seen anchor line =
    let n = String.length line in
    let emit t =
      match normalize t with
      | Some name -> add_edge p seen name anchor
      | None -> ()
    in
    let i = ref 0 in
    while !i < n do
      if !i + 1 < n && line.[!i] = ']' && line.[!i + 1] = '(' then
        (* markdown [text](dest) *)
        match String.index_from_opt line (!i + 2) ')' with
        | Some j ->
            emit (String.trim (String.sub line (!i + 2) (j - !i - 2)));
            i := j + 1
        | None -> i := n
      else if !i + 5 < n && String.sub line !i 5 = "(link" then begin
        (* (link "dest" ...): dest is a quoted sexp atom *)
        let j = ref (!i + 5) in
        while !j < n && (line.[!j] = ' ' || line.[!j] = '\t') do
          incr j
        done;
        if !j < n && line.[!j] = '"' then begin
          let buf = Buffer.create 16 in
          let k = ref (!j + 1) in
          let done_ = ref false in
          while (not !done_) && !k < n do
            if line.[!k] = '\\' && !k + 1 < n then (
              Buffer.add_char buf line.[!k + 1];
              k := !k + 2)
            else if line.[!k] = '"' then done_ := true
            else (
              Buffer.add_char buf line.[!k];
              incr k)
          done;
          emit (Buffer.contents buf);
          i := !k + 1
        end
        else incr i
      end
      else incr i
    done
  in
  List.iter
    (fun (p : Page.page) ->
      (* generated search result pages link to hundreds of pages; they
         are navigation, not references -- keep them out of backlinks *)
      if not (String.starts_with ~prefix:"search-" p.Page.name) then begin
      let seen : (string, unit) Hashtbl.t = Hashtbl.create 16 in
      let raw = p.Page.raw in
      let len = String.length raw in
      let anchor = ref None in
      let pos = ref 0 in
      while !pos < len do
        let line_end =
          match String.index_from_opt raw !pos '\n' with
          | Some nl -> nl
          | None -> len
        in
        let line = String.sub raw !pos (line_end - !pos) in
        (match Heading.parse_heading line with
         | Some (_, title) -> anchor := Some (Heading.slugify title)
         | None -> ());
        scan_line p seen !anchor line;
        pos := line_end + 1
      done
      end)
    pages;
  (* buckets were built by prepending; restore page-list order *)
  Hashtbl.filter_map_inplace (fun _ ps -> Some (List.rev ps)) edges;
  edges

let backlinks_of (pages : Page.page list) : backlinks =
  match !backlinks_cache with
  | Some (key, edges) when key == pages -> edges
  | _ ->
      let edges = build_backlinks pages in
      backlinks_cache := Some (pages, edges);
      edges

(* O(1) per rendered page (index lookup; see build_backlinks for the
   one-time O(n * L) cost). *)
let render_backlinks (ctx : context) : string =
  prof_time prof_backlinks_t @@ fun () ->
  let refs =
    match Hashtbl.find_opt (backlinks_of ctx.pages) ctx.current.name with
    | Some ps -> ps
    | None -> []
  in
  match refs with
  | [] -> ""
  | _ ->
      let items =
        List.map
          (fun (bl : backlink) ->
            let href =
              match bl.b_anchor with
              | Some a -> page_link bl.b_page ^ "#" ^ a
              | None -> page_link bl.b_page
            in
            Printf.sprintf "<a href=\"%s\">%s</a><br>" href
              (Util.html_escape bl.b_page.title))
          refs
      in
      "<div class=\"backlinks\">Backlinks<br>\n" ^ String.concat "" items ^ "</div>"

(* (backlinks "gallery" [n]) / automatic box on major pages: up to n (4)
   backlink cards at the page bottom, same card markup as (format gallery) *)
let backlink_card (ctx : context) (bl : backlink) : string =
  let href =
    match bl.b_anchor with
    | Some a -> page_link bl.b_page ^ "#" ^ a
    | None -> page_link bl.b_page
  in
  render_card ctx bl.b_page href

let backlink_gallery_html (ctx : context) (refs : backlink list)
    ~(limit : int) : string =
  let cards =
    List.filteri (fun i _ -> i < limit) refs
    |> List.map (backlink_card ctx)
  in
  "<section class=\"backlink-gallery\">\n<h2>Backlinks</h2>\n"
  ^ cardgrid cards ^ "\n</section>"

let render_backlinks_gallery (ctx : context) ~(limit : int) : string =
  match Hashtbl.find_opt (backlinks_of ctx.pages) ctx.current.name with
  | Some refs when refs <> [] -> backlink_gallery_html ctx refs ~limit
  | _ -> ""

(* render_body is defined far below (its block machinery sits after the
   eval chain); book-format chapters are only rendered via eval_call at
   runtime, long after module load, so resolve it through this late
   reference. *)
let render_body_late : (context -> string -> string) ref =
  ref (fun _ _ -> "")

(* per-chapter inline image cap in books; chapter galleries beyond this
   collapse into one '... N more photos on the full page ...' link *)
let book_image_max = 8

(* count of "(dimg " occurrences in a page body: the chapter's total
   gallery size, used for the book-more link text *)
let count_dimg (raw : string) : int =
  let n = String.length "(dimg " in
  let rec loop acc i =
    match find_sub_opt (String.sub raw i (String.length raw - i)) "(dimg " with
    | Some k -> loop (acc + 1) (i + k + n)
    | None -> acc
  in
  loop 0 0

(* stable 32-bit fnv-1a: pseudo-random but deterministic across builds,
   used to rank a chapter's images for the book sample *)
let fnv1a (s : string) : int =
  let h = ref 0x811c9dc5 in
  String.iter
    (fun c -> h := (!h lxor Char.code c) * 0x01000193 land max_int)
    s;
  !h

(* auto-generated gallery headings (from gallery-sections.py): the
   catch-all "Photos" plus the day ("2025-09-20") and hour ("13:00")
   subsections. *)
type gallery_heading = Gh_photos | Gh_dayhour

let gallery_heading (line : string) : gallery_heading option =
  let all_digits s i0 i1 =
    let rec go i = i > i1 || (s.[i] >= '0' && s.[i] <= '9' && go (i + 1)) in
    go i0
  in
  match Heading.parse_heading line with
  | Some (_, t) ->
      if t = "Photos" then Some Gh_photos
      else if
        (String.length t = 10 && t.[4] = '-' && t.[7] = '-'
         && all_digits t 0 3 && all_digits t 5 6 && all_digits t 8 9)
        || (String.length t = 5 && t.[2] = ':'
            && all_digits t 0 1 && all_digits t 3 4)
      then Some Gh_dayhour
      else None
  | None -> None

(* chapter gallery preprocessing for books: the standalone pages' split
   into Photos/day/hour subsections never shows in books (the strip is
   self-evident, headings are clutter); a gallery beyond book_image_max
   is reduced to an assorted (fnv1a(name^line)-ranked, chronologically
   presented) sample of 8 = 2 rows of 4 thumbnails, the rest collapsing
   into one 'N more photos on the full page' link.  Runs before heading
   collection, so numbering/tocs never see the drops. *)
let book_gallery_filter (name : string) (raw : string) : string =
  let total = count_dimg raw in
  let lines = String.split_on_char '\n' raw in
  let has_gallery_heading =
    List.exists (fun l -> gallery_heading l <> None) lines
  in
  if total <= book_image_max && not has_gallery_heading then raw
  else begin
    let keep : (int, unit) Hashtbl.t = Hashtbl.create 16 in
    if total > book_image_max then
      lines
      |> List.mapi (fun i l -> (i, l))
      |> List.filter (fun (_, l) ->
             String.starts_with ~prefix:"(dimg " l)
      |> List.stable_sort (fun (ia, la) (ib, lb) ->
             compare (fnv1a (name ^ la), ia) (fnv1a (name ^ lb), ib))
      |> List.iteri (fun k (i, _) ->
             if k < book_image_max then Hashtbl.replace keep i ());
    let out = Buffer.create (String.length raw) in
    let swallowed = ref 0 in
    List.iteri
      (fun i line ->
        match gallery_heading line with
        | Some _ -> ()  (* gallery headings are standalone-page-only *)
        | None ->
            if String.starts_with ~prefix:"(dimg " line then
              if total <= book_image_max || Hashtbl.mem keep i then
                Buffer.add_string out (line ^ "\n")
              else incr swallowed
            else Buffer.add_string out (line ^ "\n"))
      lines;
    if !swallowed > 0 then
      Buffer.add_string out
        (Printf.sprintf "\n... [%d more photos on the full page](/%s) ...\n"
           !swallowed name);
    Buffer.contents out
  end

(* (query ... (format book)): reading-mode concatenation of every
   matched page as one chapter, chronological by event date (published
   date and name are the tiebreak/fallback chain).  The page hosting
   the query never becomes its own chapter and nested uses (a book page
   queried from inside a chapter) render nothing.  Chapters render with
   a fresh heading state (own numbering), shifted levels (content nests
   under the chapter title), and link back to the standalone page.
   O(total chapter bytes); chapter queries hit the cross-page query
   memo. *)
let render_chapters (ctx : context) (pages : Page.page list) : string =
  let date_key (p : Page.page) =
    match List.assoc_opt "date-start" p.Page.attrs with
    | Some (d :: _) when d <> "" -> d
    | _ -> (
        match List.assoc_opt "published" p.Page.attrs with
        | Some (d :: _) -> d
        | _ -> "9999-99-99")
  in
  let chapters =
    pages
    |> List.filter (fun (p : Page.page) ->
           p.Page.name <> ctx.current.Page.name)
    |> List.sort (fun a b ->
           compare (date_key a, a.Page.name) (date_key b, b.Page.name))
  in
  if chapters = [] then
    {|<div class="map-error">no pages</div>|}
  else
    let buf = Buffer.create 4096 in
    Buffer.add_string buf "<div class=\"book\">\n";
    (* chapter label: title + (YYYY-MM) when dated -- otherwise same-city
       repeat visits (Bangkok Sep + Bangkok Nov) look like duplicates *)
    let label_of (p : Page.page) =
      match List.assoc_opt "date-start" p.Page.attrs with
      | Some (d :: _) when String.length d >= 7 ->
          Printf.sprintf "%s (%s)" p.Page.title (String.sub d 0 7)
      | _ -> p.Page.title
    in
    List.iteri
      (fun i (p : Page.page) ->
        (* drop the chapter's own top-level h1 (its title): the injected
           chapter-title takes that role -- one title per chapter *)
        let raw = Page.drop_first_h1 p.Page.raw in
        let label = label_of p in
        (* chapter titles are h2 headings and join the book page's
           numbering/dynamic-heading flow, so a (toc) on the book page
           shows chapters and their content like any other toc *)
        let cid = "chapter-" ^ p.Page.name in
        let counters2 = Heading.update_counters ctx.hstate.counters 2 in
        ctx.hstate.counters <- counters2;
        let ch_num = Heading.format_counter counters2 in
        let idx = ctx.hstate.toc_index in
        ctx.hstate.toc_index <- idx + 1;
        ctx.hstate.dynamic_headings
          <- ctx.hstate.dynamic_headings
             @ Heading.
               [{ level = 2; title = label; slug = cid;
                  number_str = ch_num ^ " " ^ label }];
        (* gallery preprocessing first: dropped hour/day sections and
           extra images must not reach heading collection or the toc *)
        let raw = book_gallery_filter p.Page.name raw in
        let headings =
          Heading.collect_headings raw
          |> List.map (fun (h : Heading.heading) ->
                 { h with Heading.level = min 6 (h.level + 1) })
        in
        let cctx =
          {
            ctx with
            current = p;
            headings;
            hstate = new_heading_state ();
            book_depth = ctx.book_depth + 1;
            book_image_count = ref 0;
            book_image_total = count_dimg raw;
            heading_offset = 1;
            backlinks_op_used = false;
          }
        in
        let body = !render_body_late cctx raw in
        Buffer.add_string buf
          (Printf.sprintf
             {|<section class="chapter" id="%s">
<h2 id="%s" style="--is: --h-%d"><a href="#%s">%s</a> <a href="/%s">%s</a></h2>
%s</section>
|}
             cid cid idx cid ch_num p.Page.name (Util.html_escape label)
             body))
      chapters;
    Buffer.add_string buf "</div>\n";
    Buffer.contents buf

let () = render_chapters_late := render_chapters

let known_inline_ops = [
  "nav"; "list-pages"; "toc"; "backlinks"; "site-name";
  "img"; "dimg"; "link"; "table"; "green"; "bold"; "em"; "code"; "quote"; "query"; "meta"; "details";
  "location"; "map"
]

(* S-expression interpreter: O(size of the sexp) plus the cost of each
   operator - query and map dominate (see render_query/Roughmap.render),
   everything else is linear in its arguments. *)
let rec eval (ctx : context) (sx : sexp) : string =
  match sx with
  | Atom a -> Util.html_escape a
  | List [] -> ""
  | List (Atom op :: args) -> eval_call ctx op args
  | List (head :: _) -> eval ctx head

(* <tag> wrapping space-separated evaluated arguments *)
and wrapped_tag (tag : string) (ctx : context) (args : sexp list) : string =
  "<" ^ tag ^ ">" ^ String.concat " " (List.map (eval ctx) args)
  ^ "</" ^ tag ^ ">"

(* cell-like inline content: atoms render markup, lists evaluate *)
and eval_item (ctx : context) (c : sexp) : string =
  match c with
  | Atom s -> render_line ctx s
  | List _ -> eval ctx c

and eval_call (ctx : context) (op : string) (args : sexp list) : string =
  (* first pass only: output-only ops render nothing (queries still run,
     so (group tag) headings are collected).  (book ...) is NOT skipped:
     its chapters emit level-shifted headings that the page's (toc)
     needs, so the first pass renders books purely for collection. *)
  if ctx.collect_only && List.mem op ["nav"; "list-pages"; "toc"; "backlinks"; "map"]
  then begin
    if op = "backlinks" then ctx.backlinks_op_used <- true;
    ""
  end
  else
  match (op, args) with
  | "nav", _ -> "<nav>" ^ render_navbar ctx ^ "</nav>"

  | "list-pages", Atom substr :: _ ->
      render_query ctx [
        List [Atom "where"; List [Atom "contains"; Atom "title"; Atom substr]];
        List [Atom "format"; Atom "list"];
      ]

  | "toc", _ -> render_toc (ctx.headings @ ctx.hstate.dynamic_headings)

  | "backlinks", args ->
      ctx.backlinks_op_used <- true;
      (match args with
       | Atom "gallery" :: rest ->
           let limit =
             match rest with
             | [ Atom n ] ->
                 Option.value ~default:4 (int_of_string_opt n)
             | _ -> 4
           in
           render_backlinks_gallery ctx ~limit
       | _ -> render_backlinks ctx)

  | "site-name", _ -> ""

  | "img", Atom url :: rest ->
    let alt = match rest with Atom a :: _ -> a | _ -> "" in
    (* optional width/height at positions 1/2 *)
    let dim_attr name i =
      match List.nth_opt rest i with
      | Some (Atom s) when s <> "" ->
          Printf.sprintf " %s=\"%s\"" name (Util.html_escape s)
      | _ -> ""
    in
    Printf.sprintf
      "<img src=\"%s\" alt=\"%s\"%s%s loading=\"lazy\" decoding=\"async\">"
      (Util.html_escape url) (Util.html_escape alt)
      (dim_attr "width" 1) (dim_attr "height" 2)

  | "dimg", Atom dither_url :: Atom real_url :: rest ->
    (* dither base img + real img overlay: BOTH loading=lazy, so nothing
       is fetched until it scrolls near the viewport; :hover flips the
       overlay's opacity and it stays flipped (css ~11.5d transition
       delay on the way back, see style.css).  rest: optional w h and
       alt.
       When the photo orig is published (published-photos.json), a
       [full-res: n MB] link is appended automatically at build time.
       Inside a book-format chapter images render as a compact 4-wide
       thumbnail strip (undithered -card.webp, clicking opens the full
       image); only the first book_image_max render, the rest collapse
       into one link to the full page. *)
    let capped =
      if ctx.book_depth > 0 then (
        incr ctx.book_image_count;
        !(ctx.book_image_count))
      else 0
    in
    (* consecutive kept images form one .bookstrip grid container:
       opened at the first kept image, closed at the last (or just
       before the more-photos link).  absolute coordinates are not
       needed: grid rows give equal tile heights, whitespace text nodes
       between anchors are dropped inside grid containers *)
    if capped > book_image_max then
      if capped = book_image_max + 1 then
        "</div>\n"
        ^ Printf.sprintf
            "<p class=\"book-more\">... <a href=\"/%s\">%d more photos \
             on the full page</a> ...</p>"
            (Util.html_escape ctx.current.Page.name)
            (ctx.book_image_total - book_image_max)
      else ""
    else if ctx.book_depth > 0 then
      Printf.sprintf
        "%s<a class=\"bookthumb\" href=\"%s\" title=\"%s\" \
         style=\"background-image:url(%s)\"></a>%s"
        (if capped = 1 then {|<div class="bookstrip">
|} else "")
        (Util.html_escape real_url)
        (Util.html_escape
           (match List.nth_opt rest 2 with
            | Some (Atom a) -> a
            | _ -> ""))
        (Util.html_escape (card_preview_url dither_url))
        (if capped = ctx.book_image_total
           && ctx.book_image_total <= book_image_max
         then "\n</div>" else "")
    else
    let get i = match List.nth_opt rest i with Some (Atom s) -> s | _ -> "" in
    let w, h = get 0, get 1 in
    let ratio =
      if w <> "" && h <> "" then
        Printf.sprintf "aspect-ratio:%s/%s;" (Util.html_escape w)
          (Util.html_escape h)
      else ""
    in
    let dim_attr name v =
      if v <> "" then Printf.sprintf " %s=\"%s\"" name (Util.html_escape v)
      else ""
    in
    Printf.sprintf
      "<div class=\"dw\" role=\"img\" aria-label=\"%s\" style=\"%s\">\
       <img class=\"dwd\" src=\"%s\"%s%s loading=\"lazy\" decoding=\"async\" alt=\"%s\">\
       <img class=\"dwr\" src=\"%s\"%s%s loading=\"lazy\" decoding=\"async\" alt=\"\" aria-hidden=\"true\">\
       </div>%s"
      (Util.html_escape (get 2)) ratio
      (Util.html_escape dither_url) (dim_attr "width" w) (dim_attr "height" h)
      (Util.html_escape (get 2))
      (Util.html_escape real_url) (dim_attr "width" w) (dim_attr "height" h)
      (fullres_link dither_url)

  | "link", Atom target :: rest ->
      let label = match rest with Atom l :: _ -> l | _ -> target in
      (* clean-URL page link; "name#frag" keeps the fragment
         (used for #:~:text= highlights from search pages) *)
      let target =
        if String.starts_with ~prefix:"/" target then
          String.sub target 1 (String.length target - 1)
        else target
      in
      let name, frag =
        match String.index_opt target '#' with
        | Some i ->
            (String.sub target 0 i,
             String.sub target i (String.length target - i))
        | None -> (target, "")
      in
      Printf.sprintf "<a href=\"/%s\">%s</a>"
        (Util.html_escape (name ^ frag)) (Util.html_escape label)

  | "table", rows ->
      let ncols =
        List.fold_left (fun mx row ->
          match row with
          | List (Atom "header" :: hdrs) -> max mx (List.length hdrs)
          | List (Atom "row" :: cells) -> max mx (List.length cells)
          | List (Atom "section" :: _) -> mx
          | List cells -> max mx (List.length cells)
          | _ -> mx) 0 rows
      in
      let cell_html c = eval_item ctx c in
      (* one <tr> of same-tagged cells *)
      let row_of tag cells =
        "  <tr>" ^ String.concat ""
          (List.map
             (fun c -> Printf.sprintf "<%s>%s</%s>" tag (cell_html c) tag)
             cells)
        ^ "</tr>\n"
      in
      let render_table_row = function
        | List (Atom "header" :: cells) -> row_of "th" cells
        | List (Atom "section" :: Atom label :: _) ->
            Printf.sprintf "  <tr><td colspan=\"%d\" class=\"section\">%s</td></tr>\n"
              ncols (render_line ctx label)
        | List (Atom "row" :: cells) -> row_of "td" cells
        | List cells -> row_of "td" cells
        | _ -> ""
      in
      "<table>\n" ^ String.concat "" (List.map render_table_row rows) ^ "</table>"

  | "green", rest ->
      let content = String.concat "" (List.map (eval_item ctx) rest) in
      "<span class=\"green\">" ^ content ^ "</span>"

  | "details", rest ->
      let content = String.concat " " (List.map (eval_item ctx) rest) in
      "<details><p>" ^ content ^ "</p></details>"

  (* <inline-tag> wrapping space-separated evaluated arguments *)
  | "bold", rest -> wrapped_tag "b" ctx rest
  | "em", rest -> wrapped_tag "em" ctx rest
  | "code", rest -> wrapped_tag "code" ctx rest
  | "quote", rest -> wrapped_tag "blockquote" ctx rest

  | "query", args -> render_query ctx args

  | "map", args -> Roughmap.render ctx.map_cache_dir args

  | "meta", _ -> ""

  (* Inline location marker: renders "City, Country" with links to the
     generated index pages; the attrs themselves are picked up by Page.parse_attrs. *)
  | "location", vals ->
      vals |> atoms |> List.map (local_link ctx) |> String.concat ", "

  | _ ->
      let args_str = String.concat " " (List.map (eval ctx) args) in
      "(" ^ Util.html_escape op ^ (if args_str = "" then "" else " " ^ args_str) ^ ")"

(* Inline-markup renderer: O(L) expected, L = line length; worst case
   O(L^2) on lines dense in failed markup candidates (each '[' or '**'
   rescans the rest of the line via String.index_from_opt).  Each '('
   candidate additionally runs parse_sexp_at, O(sexp size). *)
and render_line (ctx : context) (line : string) : string =
  let len = String.length line in
  let buf = Buffer.create len in
  let i = ref 0 in
  (* [inner](url) with the '[' at position b: (inner, url, next offset) *)
  let bracket_markup b =
    match String.index_from_opt line b ']' with
    | Some close_bracket
      when close_bracket + 1 < len && line.[close_bracket + 1] = '(' -> (
        match String.index_from_opt line (close_bracket + 1) ')' with
        | Some close_paren ->
            Some
              ( String.sub line (b + 1) (close_bracket - b - 1),
                String.sub line
                  (close_bracket + 2)
                  (close_paren - close_bracket - 2),
                close_paren + 1 )
        | None -> None)
    | _ -> None
  in
  (* apply a [inner](url) formatter; on any malformation keep the
     current char as literal text *)
  let bracket_markup_or_literal b format =
    match bracket_markup b with
    | Some (inner, url, next) ->
        Buffer.add_string buf
          (Printf.sprintf format (Util.html_escape url)
             (Util.html_escape inner));
        i := next
    | None ->
        Buffer.add_char buf line.[!i];
        incr i
  in
  while !i < len do
    let c = line.[!i] in

    (* images *)
    if c = '!' && !i + 1 < len && line.[!i + 1] = '[' then
      bracket_markup_or_literal (!i + 1)
        "<img src=\"%s\" alt=\"%s\" loading=\"lazy\" decoding=\"async\">"

    (* links *)
    else if c = '[' then
      bracket_markup_or_literal !i "<a href=\"%s\">%s</a>"

    else if c = '(' then (
      match
        prof_time prof_sxparse_t (fun () -> parse_sexp_at line !i)
      with
      | List (Atom op :: _) as sexp, next when List.mem op known_inline_ops ->
          Buffer.add_string buf (eval ctx sexp);
          i := next
      | _, _ | exception Parse_error _ ->
          Buffer.add_string buf (Util.html_escape "(");
          incr i)

    (* code *)
    else if c = '`' then (
      match String.index_from_opt line (!i + 1) '`' with
      | Some close when close > !i + 1 ->
          let inner = String.sub line (!i + 1) (close - !i - 1) in
          Buffer.add_string buf
            (Printf.sprintf "<code>%s</code>" (Util.html_escape inner));
          i := close + 1
      | _ ->
          Buffer.add_string buf (Util.html_escape "`");
          incr i)

    (* bold *)
    else if c = '*' && !i + 1 < len && line.[!i + 1] = '*' then (
      match
        let rec find j =
          if j + 1 >= len then None
          else if line.[j] = '*' && line.[j + 1] = '*' then Some j
          else find (j + 1)
        in
        find (!i + 2)
      with
      | Some close when close > !i + 2 ->
          let inner = String.sub line (!i + 2) (close - !i - 2) in
          Buffer.add_string buf
            (Printf.sprintf "<strong>%s</strong>" (Util.html_escape inner));
          i := close + 2
      | _ ->
          Buffer.add_string buf (Util.html_escape "**");
          i := !i + 2)

    else (
      Buffer.add_string buf (Util.html_escape (String.make 1 c));
      incr i)
  done;
  Buffer.contents buf

(* Returns [None] when no balanced s-expression can be read even by
   absorbing lines to EOF (unbalanced '('); the caller must then render
   the lines as regular text instead of one giant escaped blob.
   Complexity: O(s) when the sexp parses immediately; O(s^2) worst case
   when it never balances - each retry re-parses the accumulated string
   (s = total chars absorbed). *)
let parse_multiline_sexp (first_line : string) (rest_lines : string list)
    : sexp option * string list =
  let rec loop current_str lines =
    match parse_sexp_at current_str 0 with
    | sexp, _ -> (Some sexp, lines)
    | exception Parse_error _ ->
        (match lines with
         | [] -> (None, [])
         | next_line :: remaining ->
             loop (current_str ^ "\n" ^ next_line) remaining)
  in
  match loop first_line rest_lines with
  | (None, _) -> (None, rest_lines)
  | parsed -> parsed

type block = Text of string | Code of string

(* O(L); L = raw length (each line contributes to exactly one reopened
   chunk concat). *)
let split_code_blocks (raw : string) : block list =
  let lines = String.split_on_char '\n' raw in
  let rec aux in_code current_chunk acc = function
    | [] ->
        let content = String.concat "\n" (List.rev current_chunk) in
        let last_block = if in_code then Code content else Text content in
        List.rev (if content = "" then acc else last_block :: acc)
    | line :: rest ->
        let trimmed = String.trim line in
        if String.starts_with ~prefix:"```" trimmed then
          if in_code then
            let code_content = String.concat "\n" (List.rev (line :: current_chunk)) in
            aux false [] (Code code_content :: acc) rest
          else
            let text_content = String.concat "\n" (List.rev current_chunk) in
            let acc = if text_content = "" then acc else Text text_content :: acc in
            aux true [line] acc rest
        else
          aux in_code (line :: current_chunk) acc rest
  in
  aux false [] [] lines

type render_state = {
  buf : Buffer.t;
  mutable in_para : bool;
  mutable list_stack : (int * bool) list;  (* indent, is_ordered *)
}

let close_para st =
  if st.in_para then (
    Buffer.add_string st.buf "</p>\n";
    st.in_para <- false)

let open_para st =
  if not st.in_para then (
    Buffer.add_string st.buf "<p>\n";
    st.in_para <- true)

(* O(d); d = nesting depth of open lists above target_indent. *)
let close_lists_to st target_indent =
  let rec loop = function
    | (top, ordered) :: rest when top > target_indent ->
        let tag = if ordered then "ol" else "ul" in
        Buffer.add_string st.buf (Printf.sprintf "</li>\n</%s>\n" tag);
        st.list_stack <- rest;
        loop rest
    | _ -> ()
  in
  loop st.list_stack

let close_all_lists st = close_lists_to st (-1)

(* entering any block-level element ends the current paragraph and lists *)
let close_block st =
  close_para st;
  close_all_lists st

(* Renders a heading already validated by Heading.parse_heading, so the
   body and the TOC (Heading.collect_headings) agree on what counts as a
   heading.  Numbering/toc-index are threaded through ctx.hstate, shared
   with the dynamic (group tag) headings emitted by queries.
   Complexity: O(level + len title + d), d = open list depth. *)
let render_heading st (ctx : context) parsed_level title =
  close_block st;
  (* book-format chapters nest one level below the chapter title *)
  let level = min 6 (parsed_level + ctx.heading_offset) in
  ctx.hstate.counters <- Heading.update_counters ctx.hstate.counters level;
  let num_str = Heading.format_counter ctx.hstate.counters in
  let id = Heading.slugify title in
  let idx = ctx.hstate.toc_index in
  ctx.hstate.toc_index <- idx + 1;
  Printf.bprintf st.buf
    {|<h%d id="%s" style="--is: --h-%d"><a href="#%s">%s %s</a></h%d>|}
    level id idx id num_str (Util.html_escape title) level

(* O(d + L); d = closed list depth, L = item text length (render_line). *)
let render_list_entry st ctx indent ordered item_text =
  close_para st;
  close_lists_to st indent;
  let tag = if ordered then "ol" else "ul" in
  (* after close_lists_to no open list is deeper than indent, so a
     non-empty stack with indent <> top means a deeper list opens *)
  (match st.list_stack with
  | [] ->
      Buffer.add_string st.buf (Printf.sprintf "<%s>\n<li>" tag);
      st.list_stack <- [(indent, ordered)]
  | (top, _) :: _ when indent = top ->
      Buffer.add_string st.buf "</li>\n<li>"
  | _ ->
      Buffer.add_string st.buf (Printf.sprintf "\n<%s>\n<li>" tag);
      st.list_stack <- (indent, ordered) :: st.list_stack);
  Buffer.add_string st.buf (render_line ctx item_text)

(* O(L); L = code text length. *)
let render_code_block st (code_text : string) =
  close_block st;
  let lines = String.split_on_char '\n' code_text in
  let inner_lines =
    match lines with
    | first :: rest when String.starts_with ~prefix:"```" (String.trim first) ->
        let rec drop_last = function
          | [] | [_] -> []
          | x :: xs -> x :: drop_last xs
        in
        drop_last rest
    | _ -> lines
  in
  let escaped_code = Util.html_escape (String.concat "\n" inner_lines) in
  Printf.bprintf st.buf "<pre><code>%s</code></pre>\n" escaped_code

(* Block renderer: O(L) over L = block length, plus the cost of every
   evaluated sexp (queries, maps, tables).  A page with an unbalanced
   sexp opener triggers the parse_multiline_sexp fallback per line,
   O(L^2) worst case overall. *)
let render_text_block st (ctx : context) (text : string) =
  let rec process_lines lines =
    (* continued paragraph text (also used by the unbalanced-'(' fallback,
       so we take the current line rather than returning a string) *)
    let para_line line =
      open_para st;
      Buffer.add_string st.buf (render_line ctx line);
      Buffer.add_char st.buf '\n'
    in
    match lines with
    | [] -> ()
    | raw_line :: rest ->
        let line = String.trim raw_line in
        if String.length line > 0 && line.[0] = '(' then (
          match parse_multiline_sexp raw_line rest with
          | Some sexp, remaining_lines ->
              close_block st;
              Buffer.add_string st.buf (eval ctx sexp);
              Buffer.add_char st.buf '\n';
              process_lines remaining_lines
          | None, remaining_lines ->
              (* unbalanced '(': render the line as plain text rather than
                 swallowing the rest of the page into one escaped blob *)
              para_line line;
              process_lines remaining_lines)
        else if String.starts_with ~prefix:"<svg" line then (
          close_block st;
          let rec collect_svg acc = function
            | [] ->
                Buffer.add_string st.buf (String.concat "\n" (List.rev acc));
                Buffer.add_char st.buf '\n'
            | l :: more ->
                let acc' = l :: acc in
                if Util.contains_substring (String.trim l) "</svg>" then (
                  Buffer.add_string st.buf (String.concat "\n" (List.rev acc'));
                  Buffer.add_char st.buf '\n';
                  process_lines more)
                else
                  collect_svg acc' more
          in
          collect_svg [raw_line] rest)
        else if String.starts_with ~prefix:">" line then (
          close_block st;
          (* group consecutive '>' lines into a single quoted paragraph *)
          let strip_quote_marker l =
            let l = String.trim l in
            let l = Util.drop_prefix ~prefix:">" l in
            Util.drop_prefix ~prefix:" " l
          in
          let is_quote_line l =
            String.starts_with ~prefix:">" (String.trim l)
          in
          let rec collect acc = function
            | l :: more when is_quote_line l ->
                collect (strip_quote_marker l :: acc) more
            | remaining -> (List.rev acc, remaining)
          in
          let quoted_lines, remaining = collect [strip_quote_marker line] rest in
          let html =
            String.concat "<br>\n" (List.map (render_line ctx) quoted_lines)
          in
          Printf.bprintf st.buf "<blockquote>%s</blockquote>\n" html;
          process_lines remaining)
        else
          match Page.parse_list_item raw_line with
          | Some (indent, kind, item_text) ->
              render_list_entry st ctx indent (kind = Page.Ordered) item_text;
              process_lines rest
          | None ->
              close_all_lists st;
              if line = "" then
                close_para st
              else if String.starts_with ~prefix:"------" line || line = "---" then (
                close_para st;
                Buffer.add_string st.buf "<hr>\n")
              else (
                match Heading.parse_heading line with
                | Some (level, title) -> render_heading st ctx level title
                | None ->
                    (* includes '#'-lines that are no valid headings (no
                       space after '#', level > 5): plain paragraph text *)
                    para_line line);
              process_lines rest
  in
  process_lines (String.split_on_char '\n' text)

(* O(L) plus per-sexp evaluation costs; L = raw source length. *)
let render_body (ctx : context) (raw : string) : string =
  let st =
    { buf = Buffer.create (String.length raw * 2);
      in_para = false;
      list_stack = [] }
  in
  let blocks = split_code_blocks raw in
  List.iter
    (function
      | Text t -> render_text_block st ctx t
      | Code c -> render_code_block st c)
    blocks;
  close_block st;
  Buffer.contents st.buf

(* O(h + len body) plus one navbar query O(n * a); h = headings,
   n = pages, a = attrs/page.  Called once per page in the hot path. *)
let _ = render_body_late := render_body

let render_html (site_name : string) (ctx : context) (body : string) : string =
  let scope_style =
    match ctx.headings with
    | [] -> ""
    | hs ->
      let names = List.mapi (fun i _ -> Printf.sprintf "--h-%d" i) hs
                  |> String.concat ", " in
      Printf.sprintf " style=\"timeline-scope: %s\"" names
  in
  (* automatic backlinks.  Major pages (major_backlink_threshold or more
     inbound links) get a card gallery of up to 4 backlinks at the page
     bottom; smaller pages keep the sticky sidebar box at the start of
     <main>.  Both skipped when the page renders (backlinks ...) itself. *)
  let major_backlink_threshold = 3 in
  let bottom_gallery =
    if ctx.backlinks_op_used then ""
    else
      match Hashtbl.find_opt (backlinks_of ctx.pages) ctx.current.name with
      | Some refs when List.length refs >= major_backlink_threshold ->
          backlink_gallery_html ctx refs ~limit:4
      | _ -> ""
  in
  (* automatic table of contents: pages with several section headings
     get a (toc) inserted before their first section, unless they render
     one themselves *)
  let auto_toc_threshold = 3 in
  let body =
    if Util.contains_substring body {|<ul class="toc">|} then body
    else
      let hs = ctx.headings @ ctx.hstate.dynamic_headings in
      let n_sections =
        List.length
          (List.filter (fun (h : Heading.heading) -> h.level >= 2) hs)
      in
      match find_sub_opt body "<h2 " with
      | Some i when n_sections >= auto_toc_threshold ->
          String.sub body 0 i ^ render_toc hs ^ "\n"
          ^ String.sub body i (String.length body - i)
      | _ -> body
  in
  let backlinks =
    if ctx.backlinks_op_used || bottom_gallery <> "" then ""
    else render_backlinks ctx
  in
  Printf.sprintf
    {|<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s</title>
  <link rel="stylesheet" href="/style.css%s">
  <link rel="alternate" type="application/rss+xml" title="%s" href="/rss.xml">
%s
</head>
<body>
  <header>
    <a href="/">%s</a>
    <a href="https://r2wa.rs">r2wa.rs</a>
  </header>
  <nav>%s</nav>
  %s
  <main%s>
%s%s%s  </main>
  <footer>
    <br><br><br>
    <a href="https://social.emile.space/@hanemile/feed.rss" target="_blank" rel="noopener" class="icon"><img loading="lazy" class="webring" src="/rss.svg" alt="rss" height="32px"/></a>
    <a href="https://lieu.cblgh.org/" target="_blank" rel="noopener" class="icon"><img loading="lazy" class="webring" src="/lieu.svg" alt="lieu" height="32px"/></a>
    <a href="https://webring.xxiivv.com/#emile" target="_blank" rel="noopener" class="icon"><img loading="lazy" class="webring" src="/webring.svg" alt="XXIIVV" height="32px"/></a>
    <a href="https://social.emile.space/@hanemile" rel="me" target="_blank" class="icon"><img loading="lazy" class="webring" src="/activitypub.svg" alt="activitypub" height="32px"/></a>
    <a href="https://emile.space/gnirbew" class="icon"><img loading="lazy" class="webring" src="/ring.svg" alt="ring" height="32px"></a>
    <p>%s - generated with vokobe-ml</p>
  </footer>
</body>
</html>
|}
    (Util.html_escape ctx.current.title)
    (* build.sh stamps the stylesheet mtime into the environment so
       browsers re-fetch only when the css actually changed *)
    (match Sys.getenv_opt "VOKOBE_CSS_VERSION" with
     | Some v -> "?v=" ^ Util.html_escape v
     | None -> "")
    (Util.html_escape site_name)
    (match ctx.feed with
     | Some (file, tag) ->
         Printf.sprintf
           {|<link rel="alternate" type="application/rss+xml" title="%s" href="/%s">|}
           (Util.html_escape (site_name ^ " — " ^ tag)) file
     | None -> "")
    (Util.html_escape site_name)
    (render_navbar ctx)
    (render_info_bar ctx)
    scope_style
    backlinks
    body
    bottom_gallery
    (Util.html_escape site_name)
