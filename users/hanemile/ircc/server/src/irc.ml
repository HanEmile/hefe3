open! Core
open! Async
open Ircc_common
open Types

(** Minimal IRC client: enough to log into a soju bouncer network over TLS
    (SASL PLAIN), track channels/members/topics, and turn PRIVMSG/NOTICE
    into [Message.t]s. One connection per upstream network, each targeting
    the same bouncer with soju's "user/network" SASL username convention. *)

module Config = struct
  type t =
    { host : string
    ; port : int
    ; bouncer_user : string
    ; bouncer_pass : string
    ; nick : string
    ; networks : string list
    ; logs_dir : string
      (* soju's on-disk message log root, e.g. "/var/lib/soju/logs" -- logs
         live at <logs_dir>/<bouncer_user>/<network>/<channel>/<yyyy-mm-dd>.log *)
    }
end

(* One outgoing-writer per live network connection, so [send_message] (driven
   by the web client) can route an outgoing PRIVMSG to the right upstream. *)
let writers : (string, string Pipe.Writer.t) Hashtbl.t = Hashtbl.create (module String)

(* One in-flight WHOIS/MODE query at a time per (network, nick-or-channel),
   keyed case-insensitively. [*_acc] holds the partial result as numeric
   replies stream in; the ivar in [*_pending] is filled on the terminating
   reply (or a timeout) and both entries are removed then. *)
let whois_pending : (string * string, Whois_result.t Or_error.t Ivar.t) Hashtbl.Poly.t =
  Hashtbl.Poly.create ()
;;

let whois_acc : (string * string, Whois_result.t) Hashtbl.Poly.t = Hashtbl.Poly.create ()
let channel_mode_pending : (string * string, string Or_error.t Ivar.t) Hashtbl.Poly.t =
  Hashtbl.Poly.create ()
;;

(* One in-flight "load more history" request at a time per (network,
   channel), keyed case-insensitively like the tables above. [acc] collects
   the batch's messages (newest-first, as they stream in) until the BATCH end
   line fills the ivar with how many were collected; [batch_ref_to_history_key]
   lets a tagged PRIVMSG/NOTICE line find its accumulator by the batch
   reference id the server assigned, since that's all the line itself carries. *)
let history_fetch_pending
  : (string * string, Message.t list ref * int Or_error.t Ivar.t) Hashtbl.Poly.t
  =
  Hashtbl.Poly.create ()
;;

let batch_ref_to_history_key : (string, string * string) Hashtbl.Poly.t = Hashtbl.Poly.create ()

(* ------------------------------------------------------------------ *)
(* Shared, mutable, broadcast state                                   *)
(* ------------------------------------------------------------------ *)

module Global_state = struct
  type t =
    { mutable state : State.t
    ; mutable subscribers : State.t Pipe.Writer.t list
    }

  let create () = { state = State.empty; subscribers = [] }

  let broadcast t =
    t.subscribers
    <- List.filter t.subscribers ~f:(fun w ->
         if Pipe.is_closed w
         then false
         else (
           Pipe.write_without_pushback_if_open w t.state;
           true))
  ;;

  let update t ~f =
    t.state <- f t.state;
    broadcast t
  ;;

  let subscribe t =
    let r, w = Pipe.create () in
    t.subscribers <- w :: t.subscribers;
    Pipe.write_without_pushback_if_open w t.state;
    r
  ;;
end

(* ------------------------------------------------------------------ *)
(* Pure state transitions                                             *)
(* ------------------------------------------------------------------ *)

let map_network (state : State.t) network ~f =
  { state with
    networks =
      List.map state.networks ~f:(fun n ->
        if String.equal n.name network then f n else n)
  }
;;

let ensure_network (state : State.t) network =
  if List.exists state.networks ~f:(fun n -> String.equal n.name network)
  then state
  else
    { state with
      networks = state.networks @ [ { Network.name = network; connected = false; channels = [] } ]
    }
;;

let mark_connected state network ~connected =
  map_network state network ~f:(fun n -> { n with connected })
;;

let ensure_channel state network channel =
  let state = ensure_network state network in
  map_network state network ~f:(fun n ->
    if List.exists n.channels ~f:(fun c -> String.Caseless.equal c.name channel)
    then n
    else
      { n with
        channels =
          n.channels @ [ { Channel.name = channel; topic = None; members = []; messages = [] } ]
      })
;;

let remove_channel state network channel =
  map_network state network ~f:(fun n ->
    { n with channels = List.filter n.channels ~f:(fun c -> not (String.Caseless.equal c.name channel)) })
;;

let map_channel state network channel ~f =
  map_network state network ~f:(fun n ->
    { n with
      channels =
        List.map n.channels ~f:(fun c -> if String.Caseless.equal c.name channel then f c else c)
    })
;;

let set_topic state network channel topic =
  let state = ensure_channel state network channel in
  map_channel state network channel ~f:(fun c -> { c with topic = Some topic })
;;

let clear_messages state network channel =
  let state = ensure_channel state network channel in
  map_channel state network channel ~f:(fun c -> { c with messages = [] })
;;

let add_member state network channel member =
  let state = ensure_channel state network channel in
  map_channel state network channel ~f:(fun c ->
    if List.exists c.members ~f:(fun m -> String.Caseless.equal m.nick member.Member.nick)
    then c
    else { c with members = c.members @ [ member ] })
;;

let remove_member state network channel nick =
  map_channel state network channel ~f:(fun c ->
    { c with members = List.filter c.members ~f:(fun m -> not (String.Caseless.equal m.nick nick)) })
;;

let remove_member_everywhere state network nick =
  map_network state network ~f:(fun n ->
    { n with
      channels =
        List.map n.channels ~f:(fun c ->
          { c with
            members = List.filter c.members ~f:(fun m -> not (String.Caseless.equal m.nick nick))
          })
    })
;;

let rename_member state network old_nick new_nick =
  map_network state network ~f:(fun n ->
    { n with
      channels =
        List.map n.channels ~f:(fun c ->
          { c with
            members =
              List.map c.members ~f:(fun m ->
                if String.Caseless.equal m.nick old_nick then { m with nick = new_nick } else m)
          })
    })
;;

(* Cap on how far live traffic alone can grow a channel's scrollback. *)
let max_history = 5000

(* Hard ceiling after "load more" prepends old backlog on top of [max_history]
   worth of live messages -- generous, since a user has to scroll all the way
   back and request more pages by hand to ever approach it. *)
let max_total_history = 50000

(* How much backlog to pull from the bouncer right after joining, and how
   much more to pull per "scrolled to the top" load-more request. *)
let initial_history_limit = 300
let history_fetch_limit = 200

let append_message state network channel message =
  let state = ensure_channel state network channel in
  map_channel state network channel ~f:(fun c ->
    let messages = c.messages @ [ message ] in
    let messages =
      if List.length messages > max_history
      then List.drop messages (List.length messages - max_history)
      else messages
    in
    { c with messages })
;;

(* Prepends older backlog (oldest-first, like [messages]) fetched via
   CHATHISTORY BEFORE. Trims from the front (not the back) if that pushes the
   channel over [max_total_history], so it's the oldest-of-the-old that gets
   dropped rather than anything recent. *)
let prepend_messages state network channel messages =
  let state = ensure_channel state network channel in
  map_channel state network channel ~f:(fun c ->
    let combined = messages @ c.messages in
    let combined =
      if List.length combined > max_total_history
      then List.drop combined (List.length combined - max_total_history)
      else combined
    in
    { c with messages = combined })
;;

let map_channel_member state network channel nick ~f =
  map_channel state network channel ~f:(fun c ->
    { c with
      members =
        List.map c.members ~f:(fun m -> if String.Caseless.equal m.nick nick then f m else m)
    })
;;

let apply_mode_changes state network channel modestr nicks =
  let nicks = ref nicks in
  let pop () =
    match !nicks with
    | [] -> None
    | n :: rest ->
      nicks := rest;
      Some n
  in
  let state = ref state in
  let sign = ref true in
  String.iter modestr ~f:(fun ch ->
    match ch with
    | '+' -> sign := true
    | '-' -> sign := false
    | 'o' ->
      Option.iter (pop ()) ~f:(fun nick ->
        state := map_channel_member !state network channel nick ~f:(fun m -> { m with op = !sign }))
    | 'v' ->
      Option.iter (pop ()) ~f:(fun nick ->
        state
        := map_channel_member !state network channel nick ~f:(fun m -> { m with voice = !sign }))
    | _ -> ());
  !state
;;

(* ------------------------------------------------------------------ *)
(* Wire protocol: TLS transport + IRC line (de)serialization          *)
(* ------------------------------------------------------------------ *)

let connect_tls ~host ~port =
  match%bind
    Monitor.try_with (fun () ->
      Tcp.connect (Tcp.Where_to_connect.of_host_and_port { Host_and_port.host; port }))
  with
  | Error exn -> return (Or_error.of_exn exn)
  | Ok (_socket, reader, writer) ->
    let net_to_ssl = Reader.pipe reader in
    let ssl_to_net = Writer.pipe writer in
    let app_to_ssl, app_to_ssl_writer = Pipe.create () in
    let app_reader, ssl_to_app = Pipe.create () in
    (match%bind
       Async_ssl.Ssl.client ~hostname:host ~app_to_ssl ~ssl_to_app ~net_to_ssl ~ssl_to_net ()
     with
     | Error e -> return (Error e)
     | Ok (_conn : Async_ssl.Ssl.Connection.t) -> return (Ok (app_reader, app_to_ssl_writer)))
;;

(** Turn a pipe of raw byte chunks into a pipe of complete, CRLF/LF-stripped
    lines. *)
let lines_of_pipe (chunks : string Pipe.Reader.t) : string Pipe.Reader.t =
  let r, w = Pipe.create () in
  let buf = Buffer.create 4096 in
  let flush_lines () =
    let s = Buffer.contents buf in
    let parts = String.split s ~on:'\n' in
    match List.rev parts with
    | [] -> ()
    | last :: rest_rev ->
      let complete = List.rev rest_rev in
      List.iter complete ~f:(fun line ->
        let line = String.rstrip ~drop:(Char.equal '\r') line in
        Pipe.write_without_pushback_if_open w line);
      Buffer.clear buf;
      Buffer.add_string buf last
  in
  don't_wait_for
    (let%map () =
       Pipe.iter chunks ~f:(fun chunk ->
         Buffer.add_string buf chunk;
         flush_lines ();
         Deferred.unit)
     in
     Pipe.close w);
  r
;;

type parsed =
  { tags : (string * string) list
  ; prefix : string option
  ; command : string
  ; params : string list
  }

let parse_tags raw =
  if String.is_prefix raw ~prefix:"@"
  then (
    match String.lsplit2 raw ~on:' ' with
    | Some (tagstr, rest) ->
      let tags =
        String.drop_prefix tagstr 1
        |> String.split ~on:';'
        |> List.filter_map ~f:(fun kv ->
          if String.is_empty kv
          then None
          else (
            match String.lsplit2 kv ~on:'=' with
            | Some (k, v) -> Some (k, v)
            | None -> Some (kv, "")))
      in
      tags, rest
    | None -> [], "")
  else [], raw
;;

let parse_line raw : parsed option =
  let tags, raw = parse_tags raw in
  let prefix, raw =
    if String.is_prefix raw ~prefix:":"
    then (
      match String.lsplit2 raw ~on:' ' with
      | Some (p, rest) -> Some (String.drop_prefix p 1), rest
      | None -> None, raw)
    else None, raw
  in
  let rec loop raw acc =
    if String.is_empty raw
    then List.rev acc
    else (
      match String.lsplit2 raw ~on:' ' with
      | None -> List.rev (raw :: acc)
      | Some (tok, rest) ->
        if String.is_empty tok
        then loop rest acc
        else if String.is_prefix tok ~prefix:":"
        then
          List.rev
            ((String.drop_prefix tok 1 ^ if String.is_empty rest then "" else " " ^ rest) :: acc)
        else loop rest (tok :: acc))
  in
  match loop raw [] with
  | [] -> None
  | command :: params -> Some { tags; prefix; command = String.uppercase command; params }
;;

let nick_of_prefix = function
  | None -> "*"
  | Some p ->
    (match String.lsplit2 p ~on:'!' with
     | Some (nick, _) -> nick
     | None -> p)
;;

(* "\001ACTION waves\001" -> Some "waves" (a /me from this or any other client) *)
let strip_ctcp_action text =
  let ctcp_action_prefix = "\001ACTION" in
  if String.is_prefix text ~prefix:ctcp_action_prefix && String.is_suffix text ~suffix:"\001"
  then (
    let prefix_len = String.length ctcp_action_prefix in
    let inner_len = String.length text - prefix_len - 1 in
    if inner_len >= 0
    then Some (String.strip (String.sub text ~pos:prefix_len ~len:inner_len))
    else None)
  else None
;;

(* UTC throughout (both for live messages and for the "time" tag on replayed
   history, which servers always send in UTC) so the two never disagree. *)
let now_date_time () =
  let tm = Unix.gmtime (Unix.time ()) in
  let open Unix in
  let date = sprintf "%04d-%02d-%02d" (tm.tm_year + 1900) (tm.tm_mon + 1) tm.tm_mday in
  let time = sprintf "%02d:%02d" tm.tm_hour tm.tm_min in
  date, time
;;

(* "2026-10-05T10:38:54.000Z" -> ("2026-10-05", "10:38") *)
let parse_history_time time_tag =
  if String.length time_tag >= 16
  then Some (String.sub time_tag ~pos:0 ~len:10, String.sub time_tag ~pos:11 ~len:5)
  else None
;;

let parse_name prefix_map name =
  if String.is_empty name
  then { Member.nick = ""; op = false; voice = false }
  else (
    let c0 = name.[0] in
    match List.Assoc.find prefix_map c0 ~equal:Char.equal with
    | Some mode_char ->
      let nick = String.drop_prefix name 1 in
      { Member.nick; op = Char.equal mode_char 'o'; voice = Char.equal mode_char 'v' }
    | None -> { Member.nick = name; op = false; voice = false })
;;

(* soju's log lines (irssi-style):
     "[HH:MM:SS] <nick> text"      -- Chat
     "[HH:MM:SS] * nick text"      -- Action (/me)
     "[HH:MM:SS] -nick- text"      -- Notice
     "[HH:MM:SS] *** anything"     -- join/part/quit/topic/etc; no single
                                       nick field to attribute a match to, so
                                       these are skipped by the search below.
   The date isn't in the line at all -- it's the log file's own name. *)
let parse_log_line line : (string * string * string * Message_kind.t) option =
  match String.lsplit2 line ~on:']' with
  | None -> None
  | Some (bracketed, rest) ->
    let time = String.chop_prefix bracketed ~prefix:"[" |> Option.value ~default:bracketed in
    let rest = String.lstrip rest in
    if String.is_prefix rest ~prefix:"<"
    then (
      match String.lsplit2 (String.drop_prefix rest 1) ~on:'>' with
      | Some (nick, text) -> Some (time, nick, String.lstrip text, Message_kind.Chat)
      | None -> None)
    else if String.is_prefix rest ~prefix:"* "
    then (
      match String.lsplit2 (String.drop_prefix rest 2) ~on:' ' with
      | Some (nick, text) -> Some (time, nick, text, Message_kind.Action)
      | None -> None)
    else if String.is_prefix rest ~prefix:"-"
    then (
      match String.lsplit2 (String.drop_prefix rest 1) ~on:'-' with
      | Some (nick, text) -> Some (time, nick, String.lstrip text, Message_kind.Notice)
      | None -> None)
    else None
;;

let parse_prefix_isupport value =
  match String.lsplit2 value ~on:')' with
  | Some (modes, symbols) ->
    let modes = String.chop_prefix modes ~prefix:"(" |> Option.value ~default:modes in
    (try Some (List.zip_exn (String.to_list symbols) (String.to_list modes)) with
     | _ -> None)
  | None -> None
;;

(* ------------------------------------------------------------------ *)
(* Per-network connection loop                                        *)
(* ------------------------------------------------------------------ *)

let run_network (global_state : Global_state.t) (config : Config.t) ~network_name =
  let username = config.bouncer_user ^ "/" ^ network_name in
  let rec connect_loop () =
    match%bind connect_tls ~host:config.host ~port:config.port with
    | Error err ->
      Log.Global.error
        "ircc: %s: connection failed: %s"
        network_name
        (Error.to_string_hum err);
      Global_state.update global_state ~f:(fun s -> mark_connected s network_name ~connected:false);
      let%bind () = Clock.after (Time_float.Span.of_sec 30.) in
      connect_loop ()
    | Ok (reader, writer) ->
      let lines = lines_of_pipe reader in
      let send fmt =
        Printf.ksprintf (fun s -> Pipe.write_without_pushback_if_open writer (s ^ "\r\n")) fmt
      in
      (* Numeric/command replies carry a leading ":servername " prefix, so
         matching against the raw line (rather than the parsed command) would
         never see "903"/"904" etc -- go through [parse_line] instead. *)
      let rec wait_for ~f =
        match%bind Pipe.read lines with
        | `Eof -> return None
        | `Ok line ->
          (match parse_line line with
           | Some parsed when f parsed -> return (Some parsed)
           | _ -> wait_for ~f)
      in
      send "CAP LS 302";
      send "CAP REQ :sasl batch server-time draft/chathistory";
      let%bind (_ : parsed option) =
        wait_for ~f:(fun p ->
          String.equal p.command "CAP" && List.exists p.params ~f:(String.equal "ACK"))
      in
      send "AUTHENTICATE PLAIN";
      let%bind (_ : parsed option) =
        wait_for ~f:(fun p -> String.equal p.command "AUTHENTICATE")
      in
      let sasl_payload =
        Base64.encode_exn (sprintf "\000%s\000%s" username config.bouncer_pass)
      in
      send "AUTHENTICATE %s" sasl_payload;
      let%bind (_ : parsed option) =
        wait_for ~f:(fun p -> String.equal p.command "903" || String.equal p.command "904")
      in
      send "CAP END";
      send "NICK %s" config.nick;
      send "USER %s 0 * :%s" config.nick config.nick;
      Global_state.update global_state ~f:(fun s -> ensure_network s network_name);
      Hashtbl.set writers ~key:network_name ~data:writer;
      let prefix_map = ref [ '@', 'o'; '+', 'v' ] in
      let update_whois_acc nick ~f =
        let key = network_name, String.lowercase nick in
        Option.iter (Hashtbl.find whois_acc key) ~f:(fun w -> Hashtbl.set whois_acc ~key ~data:(f w))
      in
      let finish_whois nick =
        let key = network_name, String.lowercase nick in
        match Hashtbl.find whois_pending key, Hashtbl.find whois_acc key with
        | Some ivar, Some result ->
          Ivar.fill_if_empty ivar (Ok result);
          Hashtbl.remove whois_pending key;
          Hashtbl.remove whois_acc key
        | _ -> ()
      in
      let finish_channel_modes channel modestr =
        let key = network_name, String.lowercase channel in
        match Hashtbl.find channel_mode_pending key with
        | Some ivar ->
          Ivar.fill_if_empty ivar (Ok modestr);
          Hashtbl.remove channel_mode_pending key
        | None -> ()
      in
      let handle_line raw =
        match parse_line raw with
        | None -> ()
        | Some { tags; prefix; command; params } ->
          (match command, params with
           | "PING", token :: _ -> send "PONG :%s" token
           | "PING", [] -> send "PONG"
           | "001", _ ->
             Global_state.update global_state ~f:(fun s ->
               mark_connected s network_name ~connected:true)
           | "005", params ->
             List.iter params ~f:(fun p ->
               match String.chop_prefix p ~prefix:"PREFIX=" with
               | Some v ->
                 (match parse_prefix_isupport v with
                  | Some map -> prefix_map := map
                  | None -> ())
               | None -> ())
           | "JOIN", channel :: _ ->
             let nick = nick_of_prefix prefix in
             if String.Caseless.equal nick config.nick
             then (
               (* Fresh join (or soju replaying our already-joined channels on
                  reconnect): drop whatever history we were holding and pull
                  the latest backlog from the bouncer, so reconnects don't
                  duplicate messages we already have. *)
               Global_state.update global_state ~f:(fun s ->
                 clear_messages s network_name channel);
               send "CHATHISTORY LATEST %s * %d" channel initial_history_limit)
             else
               Global_state.update global_state ~f:(fun s ->
                 add_member s network_name channel { Member.nick; op = false; voice = false })
           | "PART", channel :: _ ->
             let nick = nick_of_prefix prefix in
             if String.Caseless.equal nick config.nick
             then
               Global_state.update global_state ~f:(fun s ->
                 remove_channel s network_name channel)
             else
               Global_state.update global_state ~f:(fun s ->
                 remove_member s network_name channel nick)
           | "QUIT", _ ->
             let nick = nick_of_prefix prefix in
             Global_state.update global_state ~f:(fun s ->
               remove_member_everywhere s network_name nick)
           | "NICK", new_nick :: _ ->
             let old_nick = nick_of_prefix prefix in
             Global_state.update global_state ~f:(fun s ->
               rename_member s network_name old_nick new_nick)
           | "332", _ :: channel :: topic :: _ ->
             Global_state.update global_state ~f:(fun s -> set_topic s network_name channel topic)
           | "TOPIC", channel :: topic :: _ ->
             (* Unlike the 332 numeric, a live TOPIC command has no leading
                "our nick" param. *)
             Global_state.update global_state ~f:(fun s -> set_topic s network_name channel topic)
           | "353", _ :: _ :: channel :: names :: _ ->
             (* NAMES replies for large channels can span several 353 lines;
                accumulate (dedup by nick) rather than clobbering on each
                line, since there's no reliable single point before the
                first one to clear the existing list. *)
             let members =
               String.split names ~on:' '
               |> List.filter ~f:(fun s -> not (String.is_empty s))
               |> List.map ~f:(parse_name !prefix_map)
             in
             Global_state.update global_state ~f:(fun s ->
               List.fold members ~init:s ~f:(fun s m -> add_member s network_name channel m))
           | ("PRIVMSG" | "NOTICE"), target :: raw_text :: _ ->
             let from_nick = nick_of_prefix prefix in
             let channel = if String.is_prefix target ~prefix:"#" then target else from_nick in
             let (text, kind) : string * Message_kind.t =
               match strip_ctcp_action raw_text with
               | Some action_text -> action_text, Action
               | None -> raw_text, if String.equal command "NOTICE" then Notice else Chat
             in
             let time_tag = List.Assoc.find tags "time" ~equal:String.equal in
             let date, time =
               match time_tag with
               | Some t ->
                 (match parse_history_time t with
                  | Some dt -> dt
                  | None -> now_date_time ())
               | None -> now_date_time ()
             in
             let message = { Message.date; time; nick = from_nick; text; kind; time_tag } in
             (* A line tagged with a batch reference we're tracking for a
                "load more history" request gets accumulated there instead of
                appended live -- it's older backlog, routed to be prepended
                once the whole page has arrived (see the BATCH end case
                below). Anything else (plain live traffic, or the initial
                CHATHISTORY LATEST batch on join, which we don't track)
                appends as before. *)
             let routed_to_history_fetch =
               List.Assoc.find tags "batch" ~equal:String.equal
               |> Option.bind ~f:(Hashtbl.find batch_ref_to_history_key)
               |> Option.bind ~f:(Hashtbl.find history_fetch_pending)
               |> function
               | Some (acc, _ivar) ->
                 acc := message :: !acc;
                 true
               | None -> false
             in
             if not routed_to_history_fetch
             then
               Global_state.update global_state ~f:(fun s ->
                 append_message s network_name channel message)
           | "BATCH", reftag :: rest when String.is_prefix reftag ~prefix:"+" ->
             let ref_id = String.drop_prefix reftag 1 in
             (match rest with
              | batch_type :: target :: _ when String.equal batch_type "chathistory" ->
                let key = network_name, String.lowercase target in
                if Hashtbl.mem history_fetch_pending key
                then Hashtbl.set batch_ref_to_history_key ~key:ref_id ~data:key
              | _ -> ())
           | "BATCH", reftag :: _ when String.is_prefix reftag ~prefix:"-" ->
             let ref_id = String.drop_prefix reftag 1 in
             (match Hashtbl.find batch_ref_to_history_key ref_id with
              | None -> ()
              | Some key ->
                Hashtbl.remove batch_ref_to_history_key ref_id;
                (match Hashtbl.find history_fetch_pending key with
                 | None -> ()
                 | Some (acc, ivar) -> Ivar.fill_if_empty ivar (Ok (List.length !acc))))
           | "MODE", channel :: modestr :: nicks when String.is_prefix channel ~prefix:"#" ->
             Global_state.update global_state ~f:(fun s ->
               apply_mode_changes s network_name channel modestr nicks)
           (* WHOIS replies: accumulate into [whois_acc] until 318 ends it. *)
           | "311", _ :: nick :: username :: host :: _ :: realname :: _ ->
             update_whois_acc nick ~f:(fun w -> { w with username = Some username; host = Some host; realname = Some realname })
           | "312", _ :: nick :: server :: _ ->
             update_whois_acc nick ~f:(fun w -> { w with server = Some server })
           | "301", _ :: nick :: away :: _ ->
             update_whois_acc nick ~f:(fun w -> { w with away = Some away })
           | "330", _ :: nick :: account :: _ ->
             update_whois_acc nick ~f:(fun w -> { w with account = Some account })
           | "319", _ :: nick :: channels :: _ ->
             update_whois_acc nick ~f:(fun w ->
               { w with
                 channels = String.split channels ~on:' ' |> List.filter ~f:(fun s -> not (String.is_empty s))
               })
           | "318", _ :: nick :: _ -> finish_whois nick
           | "324", _ :: channel :: modestr :: _ -> finish_channel_modes channel modestr
           | _ -> ())
      in
      let%bind () = Pipe.iter lines ~f:(fun line -> handle_line line; Deferred.unit) in
      Hashtbl.remove writers network_name;
      Global_state.update global_state ~f:(fun s -> mark_connected s network_name ~connected:false);
      let%bind () = Clock.after (Time_float.Span.of_sec 10.) in
      connect_loop ()
  in
  connect_loop ()
;;

(* A write/read error on one network's connection (e.g. a broken pipe after
   the upstream drops us) would otherwise raise to the top-level monitor and
   take down the whole process -- including the HTTP server and every other
   network's connection. Catch and retry per network instead. *)
let rec run_network_safe global_state config ~network_name =
  match%bind Monitor.try_with (fun () -> run_network global_state config ~network_name) with
  | Ok () -> return ()
  | Error exn ->
    Log.Global.error "ircc: %s: crashed, retrying: %s" network_name (Exn.to_string exn);
    Global_state.update global_state ~f:(fun s -> mark_connected s network_name ~connected:false);
    let%bind () = Clock.after (Time_float.Span.of_sec 10.) in
    run_network_safe global_state config ~network_name
;;

let run (global_state : Global_state.t) (config : Config.t) =
  List.iter config.networks ~f:(fun network_name ->
    don't_wait_for (run_network_safe global_state config ~network_name))
;;

(* ------------------------------------------------------------------ *)
(* Outgoing messages from the web client                              *)
(* ------------------------------------------------------------------ *)

let send_message
  (global_state : Global_state.t)
  ~nick
  ~(target : Target.t)
  ~text
  ~(kind : Message_kind.t)
  =
  match Hashtbl.find writers target.network with
  | None -> Deferred.return (Or_error.error_string "network not connected")
  | Some writer ->
    (* A literal CR/LF would let the compose box inject extra IRC lines. *)
    let text = String.tr text ~target:'\r' ~replacement:' ' |> String.tr ~target:'\n' ~replacement:' ' in
    let wire_line =
      match kind with
      | Notice -> sprintf "NOTICE %s :%s\r\n" target.channel text
      | Action -> sprintf "PRIVMSG %s :\001ACTION %s\001\r\n" target.channel text
      | Chat | Join | Part | Quit | Nick_change | Status ->
        sprintf "PRIVMSG %s :%s\r\n" target.channel text
    in
    Pipe.write_without_pushback_if_open writer wire_line;
    let date, time = now_date_time () in
    Global_state.update global_state ~f:(fun s ->
      append_message
        s
        target.network
        target.channel
        { Message.date; time; nick; text; kind; time_tag = None });
    Deferred.return (Ok ())
;;

let do_raw_command ~network ~line =
  match Hashtbl.find writers network with
  | None -> Deferred.return (Or_error.error_string "network not connected")
  | Some writer ->
    let line = String.tr line ~target:'\r' ~replacement:' ' |> String.tr ~target:'\n' ~replacement:' ' in
    Pipe.write_without_pushback_if_open writer (line ^ "\r\n");
    Deferred.return (Ok ())
;;

let whois_query_timeout = Time_float.Span.of_sec 10.
let channel_modes_query_timeout = Time_float.Span.of_sec 5.

let do_whois ~network ~nick =
  match Hashtbl.find writers network with
  | None -> return (Or_error.error_string "network not connected")
  | Some writer ->
    let key = network, String.lowercase nick in
    let ivar = Ivar.create () in
    Hashtbl.set whois_pending ~key ~data:ivar;
    Hashtbl.set
      whois_acc
      ~key
      ~data:
        { Whois_result.nick
        ; username = None
        ; host = None
        ; realname = None
        ; server = None
        ; account = None
        ; away = None
        ; channels = []
        };
    Pipe.write_without_pushback_if_open writer (sprintf "WHOIS %s\r\n" nick);
    let%bind result =
      Deferred.any
        [ Ivar.read ivar
        ; (Clock.after whois_query_timeout >>| fun () -> Or_error.error_string "WHOIS timed out")
        ]
    in
    Hashtbl.remove whois_pending key;
    Hashtbl.remove whois_acc key;
    return result
;;

let history_fetch_timeout = Time_float.Span.of_sec 10.

(* Oldest-loaded-message's raw time tag for [network]/[channel], used to
   anchor a CHATHISTORY BEFORE request -- walks from the front of [messages]
   (oldest first) to the first one that actually has a tag, since a locally
   echoed message (no tag) could in principle be the oldest thing loaded. *)
let oldest_time_tag (state : State.t) network channel =
  List.find_map state.networks ~f:(fun n ->
    if String.equal n.name network
    then
      List.find_map n.channels ~f:(fun c ->
        if String.Caseless.equal c.name channel
        then List.find_map c.messages ~f:(fun m -> m.Message.time_tag)
        else None)
    else None)
;;

let do_fetch_history (global_state : Global_state.t) ~network ~channel =
  let key = network, String.lowercase channel in
  if Hashtbl.mem history_fetch_pending key
  then return (Or_error.error_string "a history fetch for this channel is already in progress")
  else (
    match Hashtbl.find writers network with
    | None -> return (Or_error.error_string "network not connected")
    | Some writer ->
      (match oldest_time_tag global_state.state network channel with
       | None -> return (Ok 0)
       | Some anchor_tag ->
         let acc = ref [] in
         let ivar = Ivar.create () in
         Hashtbl.set history_fetch_pending ~key ~data:(acc, ivar);
         Pipe.write_without_pushback_if_open
           writer
           (sprintf "CHATHISTORY BEFORE %s timestamp=%s %d\r\n" channel anchor_tag history_fetch_limit);
         let%bind result =
           Deferred.any
             [ Ivar.read ivar
             ; (Clock.after history_fetch_timeout >>| fun () -> Ok 0)
             ]
         in
         Hashtbl.remove history_fetch_pending key;
         (match result with
          | Ok _ ->
            let messages = List.rev !acc in
            if not (List.is_empty messages)
            then
              Global_state.update global_state ~f:(fun s ->
                prepend_messages s network channel messages)
          | Error _ -> ());
         return result))
;;

let log_hit_limit = 200

let ls_dir_or_empty dir =
  match%map Monitor.try_with (fun () -> Sys.ls_dir dir) with
  | Ok entries -> entries
  | Error _ -> []
;;

(* Walks <logs_dir>/<bouncer_user>/<network>/<channel>/<date>.log, matching
   [query] against each parseable line's "nick text" (status lines like
   joins/parts have no nick to attribute a match to, so they're skipped
   entirely -- see [parse_log_line]). A multi-word query ANDs its words --
   each must appear somewhere in "nick text" -- but each individual word
   must match as a literal run of consecutive characters, not a scattered
   subsequence: a search for "ctf" should not hit on a nick ending in "c"
   plus a message that happens to contain "tf" elsewhere. Reads everything on
   every call rather than maintaining an index -- soju's logs for one
   personal account run a few MB, so a full scan is still well under a
   second, and this is a rarely-invoked, interactively-triggered feature. *)
let do_search_logs ~logs_dir ~bouncer_user ~query =
  let query_words =
    String.lowercase (String.strip query)
    |> String.split ~on:' '
    |> List.filter ~f:(fun w -> not (String.is_empty w))
  in
  if List.is_empty query_words
  then return (Or_error.error_string "empty search query")
  else (
    let user_dir = Filename.concat logs_dir bouncer_user in
    let%bind networks = ls_dir_or_empty user_dir in
    let%bind hits =
      Deferred.List.concat_map networks ~how:`Sequential ~f:(fun network ->
        let network_dir = Filename.concat user_dir network in
        let%bind channels = ls_dir_or_empty network_dir in
        Deferred.List.concat_map channels ~how:`Sequential ~f:(fun channel ->
          let channel_dir = Filename.concat network_dir channel in
          let%bind files = ls_dir_or_empty channel_dir in
          Deferred.List.concat_map files ~how:`Sequential ~f:(fun file ->
            match String.chop_suffix file ~suffix:".log" with
            | None -> return []
            | Some date ->
              let%map lines =
                match%map
                  Monitor.try_with (fun () ->
                    Reader.file_lines (Filename.concat channel_dir file))
                with
                | Ok lines -> lines
                | Error _ -> []
              in
              List.filter_map lines ~f:(fun line ->
                Option.bind (parse_log_line line) ~f:(fun (time, nick, text, kind) ->
                  let target = String.lowercase (nick ^ " " ^ text) in
                  if List.for_all query_words ~f:(fun w -> String.is_substring target ~substring:w)
                  then Some { Log_hit.network; channel; date; time; nick; text; kind }
                  else None)))))
    in
    let sorted =
      List.sort hits ~compare:(fun a b ->
        [%compare: string * string]
          (b.Log_hit.date, b.Log_hit.time)
          (a.Log_hit.date, a.Log_hit.time))
    in
    return (Ok (List.take sorted log_hit_limit)))
;;

let do_channel_modes ~network ~channel =
  match Hashtbl.find writers network with
  | None -> return (Or_error.error_string "network not connected")
  | Some writer ->
    let key = network, String.lowercase channel in
    let ivar = Ivar.create () in
    Hashtbl.set channel_mode_pending ~key ~data:ivar;
    Pipe.write_without_pushback_if_open writer (sprintf "MODE %s\r\n" channel);
    let%bind result =
      Deferred.any
        [ Ivar.read ivar
        ; (Clock.after channel_modes_query_timeout
           >>| fun () -> Or_error.error_string "MODE query timed out")
        ]
    in
    Hashtbl.remove channel_mode_pending key;
    return result
;;
