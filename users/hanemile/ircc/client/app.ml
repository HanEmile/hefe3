open! Core
open! Bonsai_web
open Ircc_common
open Types
open Bonsai.Cont.Let_syntax

(* Persisted across reloads in the browser's localStorage, so read/unread
   state survives a page refresh instead of flagging the whole backlog as
   unread again every time. *)
let read_counts_storage_key = "ircc_read_counts"

let load_read_counts () : (Target.t * int) list =
  match Js_of_ocaml.Js.Optdef.to_option Js_of_ocaml.Dom_html.window##.localStorage with
  | None -> []
  | Some storage ->
    (match
       Js_of_ocaml.Js.Opt.to_option (storage##getItem (Js_of_ocaml.Js.string read_counts_storage_key))
     with
     | None -> []
     | Some s ->
       (try
          Sexp.of_string (Js_of_ocaml.Js.to_string s) |> [%of_sexp: (Target.t * int) list]
        with
        | _ -> []))
;;

let save_read_counts (read_counts : (Target.t * int) list) =
  match Js_of_ocaml.Js.Optdef.to_option Js_of_ocaml.Dom_html.window##.localStorage with
  | None -> ()
  | Some storage ->
    let s = [%sexp_of: (Target.t * int) list] read_counts |> Sexp.to_string in
    storage##setItem (Js_of_ocaml.Js.string read_counts_storage_key) (Js_of_ocaml.Js.string s)
;;

(* The global Cmd+K / Ctrl+N / Ctrl+P keydown listener (installed once, on
   [Dom_html.window], from main.ml) dispatches here. A plain [Vdom.Attr]
   listener on our own container only fires while focus happens to be
   somewhere inside that container's DOM subtree -- once focus lands
   elsewhere (e.g. on <body>, which is an ancestor, not a descendant), it
   stops bubbling to us at all, which is why a per-element listener felt
   flaky. This ref is overwritten with the latest closure on every render. *)
let current_keydown_handler
  : (Js_of_ocaml.Dom_html.keyboardEvent Js_of_ocaml.Js.t -> unit Vdom.Effect.t) ref
  =
  ref (fun _ -> Vdom.Effect.Ignore)
;;

let nick_color nick =
  let hash = String.fold nick ~init:0 ~f:(fun acc c -> (acc * 31) + Char.to_int c) in
  let hue = abs (hash mod 360) in
  sprintf "hsl(%d, 90%%, 62%%)" hue
;;

let member_sigil (m : Member.t) = if m.op then "@" else if m.voice then "+" else ""

let member_sort_key (m : Member.t) =
  (if m.op then 0 else if m.voice then 1 else 2), String.lowercase m.nick
;;

let member_compare a b =
  let rank_a, nick_a = member_sort_key a in
  let rank_b, nick_b = member_sort_key b in
  match Int.compare rank_a rank_b with
  | 0 -> String.compare nick_a nick_b
  | c -> c
;;

(** Right-side popover shown when clicking a nick (WHOIS + "open query") or
    right-clicking a channel (mode info). One piece of client-only state;
    fetched data lives here, not in the server-pushed [State.t]. *)
module Panel = struct
  type user_status =
    | Whois_loading
    | Whois_error of string
    | Whois_loaded of Whois_result.t
  [@@deriving equal]

  type channel_status =
    | Modes_loading
    | Modes_error of string
    | Modes_loaded of string
  [@@deriving equal]

  type search_status =
    | Search_loading
    | Search_error of string
    | Search_loaded of Log_hit.t list
  [@@deriving equal]

  type t =
    | Hidden
    | User_info of
        { network : string
        ; nick : string
        ; status : user_status
        }
    | Channel_info of
        { network : string
        ; channel : string
        ; status : channel_status
        }
    | Search_results of
        { query : string
        ; status : search_status
        }
  [@@deriving equal]
end

(** Per-channel "load more history" status, tracked client-side so scrolling
    to the top of an already-loading or already-exhausted channel's
    scrollback is a no-op instead of firing another request. *)
module History_status = struct
  type t =
    | Loading
    | Exhausted
  [@@deriving equal]
end

(* Scrolled within [history_fetch_threshold_px] of the top of #chat-scroll
   triggers a "load more history" fetch for the selected channel. *)
let history_fetch_threshold_px = 80

(* After older messages are prepended, the DOM grows above whatever was on
   screen; left alone, the browser keeps [scrollTop] fixed, so the content
   the user was reading jumps out of view below the fold. Bump [scrollTop] by
   however much [#chat-scroll]'s content grew, once Bonsai's own (also
   rAF-scheduled, per the comment in main.ml) patch has actually landed --
   two animation frames, same trick used for palette focus below. *)
let preserve_scroll_after_history_load ~old_scroll_height =
  let request_animation_frame f =
    ignore
      (Js_of_ocaml.Dom_html.window##requestAnimationFrame
         (Js_of_ocaml.Js.wrap_callback (fun (_ : float) -> f ())))
  in
  request_animation_frame (fun () ->
    request_animation_frame (fun () ->
      match Js_of_ocaml.Dom_html.getElementById_opt "chat-scroll" with
      | None -> ()
      | Some el ->
        let delta = el##.scrollHeight - old_scroll_height in
        if delta > 0 then el##.scrollTop := el##.scrollTop + delta))
;;

(** Cmd+K / Ctrl+K command palette: fuzzy-filterable list of channels (jump
    to them) and slash commands (prefill the compose box with them, since
    almost all of them need an argument typed in anyway). *)
module Palette = struct
  type action =
    | Switch of Target.t
    | Prefill of string

  type item =
    { label : string
    ; sublabel : string option
    ; action : action
    }

  let command_items =
    List.map
      [ "/join", "join a channel"
      ; "/part", "part the current channel"
      ; "/msg", "message a user or service (NickServ, ChanServ, BouncerServ, ...)"
      ; "/notice", "send a notice"
      ; "/me", "action message"
      ; "/nick", "change nickname"
      ; "/topic", "view or set the topic"
      ; "/whois", "look up a user"
      ; "/query", "open a DM"
      ; "/mode", "view or change channel/user modes"
      ; "/invite", "invite a user to the current channel"
      ; "/away", "set/clear away status"
      ; "/raw", "send a raw IRC line"
      ; "/search", "full-text search the logs across every network/channel"
      ]
      ~f:(fun (cmd, desc) -> { label = cmd; sublabel = Some desc; action = Prefill (cmd ^ " ") })
  ;;

  let channel_items (state : State.t) =
    List.concat_map state.networks ~f:(fun (n : Network.t) ->
      List.map n.channels ~f:(fun (c : Channel.t) ->
        { label = c.name
        ; sublabel = Some n.name
        ; action = Switch { Target.network = n.name; channel = c.name }
        }))
  ;;

  let matches item query =
    String.is_empty query
    || String.is_substring (String.lowercase item.label) ~substring:(String.lowercase query)
    || (match item.sublabel with
        | Some s -> String.is_substring (String.lowercase s) ~substring:(String.lowercase query)
        | None -> false)
  ;;

  let max_results = 20
end

let view_message ~network ~on_nick_click (msg : Message.t) =
  let kind_class =
    match msg.kind with
    | Chat -> "chat-line"
    | Notice -> "chat-line chat-notice"
    | Action -> "chat-line chat-action"
    | Join -> "chat-line chat-status"
    | Part -> "chat-line chat-status"
    | Quit -> "chat-line chat-status"
    | Nick_change -> "chat-line chat-status"
    | Status -> "chat-line chat-status"
  in
  let action_star =
    match msg.kind with
    | Action -> [ Vdom.Node.text "* " ]
    | _ -> []
  in
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.class_ kind_class ]
    ([ Vdom.Node.span
         ~attrs:[ Vdom.Attr.class_ "chat-date" ]
         [ Vdom.Node.textf "%s %s" msg.date msg.time ]
     ]
     @ action_star
     @ [ Vdom.Node.span
           ~attrs:
             [ Vdom.Attr.class_ "chat-nick"
             ; Vdom.Attr.create "style" (sprintf "color: %s" (nick_color msg.nick))
             ; Vdom.Attr.on_click (fun _ -> on_nick_click network msg.nick)
             ]
           [ Vdom.Node.text msg.nick ]
       ; Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "chat-text" ] [ Vdom.Node.text msg.text ]
       ])
;;

let channel_view ~selected ~on_select ~on_contextmenu ~my_nick ~read_counts (network : Network.t) (channel : Channel.t) =
  let target = { Target.network = network.name; channel = channel.name } in
  let is_selected = [%equal: Target.t option] selected (Some target) in
  let unread, mentions =
    if is_selected
    then 0, 0
    else (
      let read = List.Assoc.find read_counts target ~equal:[%equal: Target.t] |> Option.value ~default:0 in
      let unread_msgs = List.drop channel.messages read in
      let mentions =
        if String.is_empty my_nick
        then 0
        else
          List.count unread_msgs ~f:(fun (m : Message.t) ->
            String.is_substring (String.lowercase m.text) ~substring:(String.lowercase my_nick))
      in
      List.length unread_msgs, mentions)
  in
  let classes =
    [ "channel-name" ]
    @ (if is_selected then [ "selected" ] else [])
    @ if unread > 0 then [ "unread" ] else []
  in
  Vdom.Node.div
    ~attrs:
      [ Vdom.Attr.classes classes
      ; Vdom.Attr.on_click (fun _ -> on_select target)
      ; Vdom.Attr.on_contextmenu (fun ev ->
          Js_of_ocaml.Dom.preventDefault ev;
          on_contextmenu network.name channel.name)
      ]
    ([ Vdom.Node.text channel.name ]
     @
     if mentions > 0
     then [ Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "mention-badge" ] [ Vdom.Node.textf "%d" mentions ] ]
     else [])
;;

let network_view ~selected ~on_select ~on_contextmenu ~my_nick ~read_counts (network : Network.t) =
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.class_ "network-group" ]
    (Vdom.Node.div
       ~attrs:
         [ Vdom.Attr.class_
             (if network.connected then "network-name connected" else "network-name disconnected")
         ]
       [ Vdom.Node.text network.name ]
     :: List.map
          network.channels
          ~f:(channel_view ~selected ~on_select ~on_contextmenu ~my_nick ~read_counts network))
;;

let left_sidebar ~(state : State.t) ~selected ~on_select ~on_contextmenu ~read_counts =
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.id "sidebar-left" ]
    (List.map
       state.networks
       ~f:(network_view ~selected ~on_select ~on_contextmenu ~my_nick:state.my_nick ~read_counts))
;;

let right_sidebar ~network ~on_member_click (channel : Channel.t option) =
  let members =
    match channel with
    | None -> []
    | Some c -> List.sort c.members ~compare:member_compare
  in
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.id "sidebar-right" ]
    (List.map members ~f:(fun m ->
       Vdom.Node.div
         ~attrs:[ Vdom.Attr.class_ "member"; Vdom.Attr.on_click (fun _ -> on_member_click network m.nick) ]
         [ Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "member-sigil" ] [ Vdom.Node.text (member_sigil m) ]
         ; Vdom.Node.span
             ~attrs:
               [ Vdom.Attr.create "style" (sprintf "color: %s" (nick_color m.nick)) ]
             [ Vdom.Node.text m.nick ]
         ]))
;;

let compose ~compose_text ~set_compose_text ~on_submit =
  let do_send = Vdom.Effect.Many [ on_submit compose_text; set_compose_text "" ] in
  let on_keypress =
    Vdom.Attr.on_keypress (fun ev ->
      let is_enter =
        match Js_of_ocaml.Js.Optdef.to_option ev##.code with
        | None -> false
        | Some code -> String.equal (Js_of_ocaml.Js.to_string code) "Enter"
      in
      if is_enter then do_send else Vdom.Effect.Ignore)
  in
  let on_input = Vdom.Attr.on_input (fun _ text -> set_compose_text text) in
  let value = Vdom.Attr.string_property "value" compose_text in
  let placeholder = Vdom.Attr.create "placeholder" "message, or /msg /join /part /nick /me /notice /whois /query /topic /raw ..." in
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.id "compose" ]
    [ (Vdom.Node.input_deprecated [@alert "-deprecated"])
        ~attrs:[ Vdom.Attr.id "compose-input"; on_keypress; on_input; value; placeholder ]
        []
    ; Vdom.Node.button ~attrs:[ Vdom.Attr.on_click (fun _ -> do_send) ] [ Vdom.Node.text "send" ]
    ]
;;

let chat_panel
  ~(channel : Channel.t option)
  ~network
  ~on_nick_click
  ~compose_text
  ~set_compose_text
  ~on_submit
  ~on_chat_scroll
  =
  let header_name, header_topic =
    match channel with
    | None -> "", ""
    | Some c -> c.name, Option.value c.topic ~default:""
  in
  let header =
    Vdom.Node.div
      ~attrs:[ Vdom.Attr.id "chat-header" ]
      [ Vdom.Node.text header_name
      ; Vdom.Node.span ~attrs:[ Vdom.Attr.id "chat-topic" ] [ Vdom.Node.text header_topic ]
      ]
  in
  let messages =
    match channel with
    | None ->
      [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "chat-empty" ] [ Vdom.Node.text "Select a channel" ] ]
    | Some c -> List.map c.messages ~f:(view_message ~network ~on_nick_click)
  in
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.id "chat-panel" ]
    [ header
    ; Vdom.Node.div ~attrs:[ Vdom.Attr.id "chat-scroll"; on_chat_scroll ] messages
    ; compose ~compose_text ~set_compose_text ~on_submit
    ]
;;

let find_channel (state : State.t) (target : Target.t) =
  List.find state.networks ~f:(fun n -> String.equal n.name target.network)
  |> Option.bind ~f:(fun n -> List.find n.channels ~f:(fun c -> String.equal c.name target.channel))
;;

let current_channel (state : State.t) (selected : Target.t option) =
  Option.bind selected ~f:(find_channel state)
;;

let message_count (state : State.t) (target : Target.t) =
  find_channel state target |> Option.value_map ~default:0 ~f:(fun (c : Channel.t) -> List.length c.messages)
;;

let panel_row label value =
  match value with
  | None -> []
  | Some v ->
    [ Vdom.Node.div
        ~attrs:[ Vdom.Attr.class_ "panel-row" ]
        [ Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "panel-label" ] [ Vdom.Node.text label ]
        ; Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "panel-value" ] [ Vdom.Node.text v ]
        ]
    ]
;;

let render_panel (panel : Panel.t) ~set_panel ~on_select =
  match panel with
  | Panel.Hidden -> []
  | Panel.User_info { network; nick; status } ->
    let body =
      match status with
      | Panel.Whois_loading -> [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-loading" ] [ Vdom.Node.text "loading..." ] ]
      | Panel.Whois_error e ->
        [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-error" ] [ Vdom.Node.text e ] ]
      | Panel.Whois_loaded w ->
        List.concat
          [ panel_row "user" (Option.map2 w.username w.host ~f:(fun u h -> sprintf "%s@%s" u h))
          ; panel_row "name" w.realname
          ; panel_row "server" w.server
          ; panel_row "account" w.account
          ; panel_row "away" w.away
          ; panel_row
              "channels"
              (if List.is_empty w.channels then None else Some (String.concat ~sep:" " w.channels))
          ]
    in
    let open_query =
      Vdom.Attr.on_click (fun _ ->
        Vdom.Effect.Many [ on_select { Target.network; channel = nick }; set_panel Panel.Hidden ])
    in
    [ Vdom.Node.div
        ~attrs:[ Vdom.Attr.id "info-panel" ]
        ([ Vdom.Node.div
             ~attrs:[ Vdom.Attr.class_ "panel-header" ]
             [ Vdom.Node.text nick
             ; Vdom.Node.button
                 ~attrs:[ Vdom.Attr.class_ "panel-close"; Vdom.Attr.on_click (fun _ -> set_panel Panel.Hidden) ]
                 [ Vdom.Node.text "x" ]
             ]
         ]
         @ body
         @ [ Vdom.Node.div
               ~attrs:[ Vdom.Attr.class_ "panel-actions" ]
               [ Vdom.Node.button ~attrs:[ open_query ] [ Vdom.Node.text "open query" ] ]
           ])
    ]
  | Panel.Channel_info { network = _; channel; status } ->
    let body =
      match status with
      | Panel.Modes_loading -> [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-loading" ] [ Vdom.Node.text "loading..." ] ]
      | Panel.Modes_error e ->
        [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-error" ] [ Vdom.Node.text e ] ]
      | Panel.Modes_loaded m -> panel_row "modes" (Some m)
    in
    [ Vdom.Node.div
        ~attrs:[ Vdom.Attr.id "info-panel" ]
        ([ Vdom.Node.div
             ~attrs:[ Vdom.Attr.class_ "panel-header" ]
             [ Vdom.Node.text channel
             ; Vdom.Node.button
                 ~attrs:[ Vdom.Attr.class_ "panel-close"; Vdom.Attr.on_click (fun _ -> set_panel Panel.Hidden) ]
                 [ Vdom.Node.text "x" ]
             ]
         ]
         @ body)
    ]
  | Panel.Search_results { query; status } ->
    let body =
      match status with
      | Panel.Search_loading ->
        [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-loading" ] [ Vdom.Node.text "searching..." ] ]
      | Panel.Search_error e ->
        [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-error" ] [ Vdom.Node.text e ] ]
      | Panel.Search_loaded [] ->
        [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "panel-loading" ] [ Vdom.Node.text "no matches" ] ]
      | Panel.Search_loaded hits ->
        List.map hits ~f:(fun (hit : Log_hit.t) ->
          Vdom.Node.div
            ~attrs:
              [ Vdom.Attr.class_ "search-hit"
              ; Vdom.Attr.on_click (fun _ ->
                  Vdom.Effect.Many
                    [ on_select { Target.network = hit.network; channel = hit.channel }
                    ; set_panel Panel.Hidden
                    ])
              ]
            [ Vdom.Node.div
                ~attrs:[ Vdom.Attr.class_ "search-hit-meta" ]
                [ Vdom.Node.textf "%s %s  %s/%s" hit.date hit.time hit.network hit.channel ]
            ; Vdom.Node.div
                ~attrs:[ Vdom.Attr.class_ "search-hit-text" ]
                [ Vdom.Node.span
                    ~attrs:
                      [ Vdom.Attr.class_ "chat-nick"
                      ; Vdom.Attr.create "style" (sprintf "color: %s" (nick_color hit.nick))
                      ]
                    [ Vdom.Node.text hit.nick ]
                ; Vdom.Node.text (" " ^ hit.text)
                ]
            ])
    in
    [ Vdom.Node.div
        ~attrs:[ Vdom.Attr.id "search-panel" ]
        ([ Vdom.Node.div
             ~attrs:[ Vdom.Attr.class_ "panel-header" ]
             [ Vdom.Node.textf "search: %s" query
             ; Vdom.Node.button
                 ~attrs:[ Vdom.Attr.class_ "panel-close"; Vdom.Attr.on_click (fun _ -> set_panel Panel.Hidden) ]
                 [ Vdom.Node.text "x" ]
             ]
         ]
         @ [ Vdom.Node.div ~attrs:[ Vdom.Attr.id "search-results" ] body ])
    ]
;;

let render_palette
  ~palette_open
  ~palette_query
  ~set_palette_query
  ~palette_index
  ~set_palette_index
  ~filtered_items
  ~close_palette
  ~run_item
  ~focus_attr
  =
  if not palette_open
  then []
  else (
    let stop_click =
      Vdom.Attr.on_click (fun ev -> Js_of_ocaml.Dom_html.stopPropagation ev; Vdom.Effect.Ignore)
    in
    let items =
      List.mapi filtered_items ~f:(fun i (item : Palette.item) ->
        Vdom.Node.div
          ~attrs:
            [ Vdom.Attr.class_ (if i = palette_index then "palette-item selected" else "palette-item")
            ; Vdom.Attr.on_click (fun _ -> run_item item)
            ]
          ([ Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "palette-item-label" ] [ Vdom.Node.text item.label ] ]
           @
           match item.sublabel with
           | None -> []
           | Some s -> [ Vdom.Node.span ~attrs:[ Vdom.Attr.class_ "palette-item-sub" ] [ Vdom.Node.text s ] ]))
    in
    let items =
      if List.is_empty items
      then [ Vdom.Node.div ~attrs:[ Vdom.Attr.class_ "palette-empty" ] [ Vdom.Node.text "no matches" ] ]
      else items
    in
    [ Vdom.Node.div
        ~attrs:[ Vdom.Attr.id "palette-backdrop"; Vdom.Attr.on_click (fun _ -> close_palette) ]
        [ Vdom.Node.div
            ~attrs:[ Vdom.Attr.id "palette"; stop_click ]
            [ (Vdom.Node.input_deprecated [@alert "-deprecated"])
                ~attrs:
                  [ Vdom.Attr.id "palette-input"
                  ; focus_attr
                  ; Vdom.Attr.create "placeholder" "switch channel or run a command..."
                  ; Vdom.Attr.string_property "value" palette_query
                  ; Vdom.Attr.on_input (fun _ text ->
                      Vdom.Effect.Many [ set_palette_query text; set_palette_index 0 ])
                  ]
                []
            ; Vdom.Node.div ~attrs:[ Vdom.Attr.id "palette-list" ] items
            ]
        ]
    ])
;;

let component ~state ~send_message ~raw_command ~whois ~channel_modes ~fetch_history ~search_logs graph =
  let selected, set_selected = Bonsai.Cont.state ~equal:[%equal: Target.t option] None graph in
  let compose_text, set_compose_text = Bonsai.Cont.state ~equal:[%equal: string] "" graph in
  let panel, set_panel = Bonsai.Cont.state ~equal:[%equal: Panel.t] Panel.Hidden graph in
  let palette_open, set_palette_open = Bonsai.Cont.state ~equal:[%equal: bool] false graph in
  let palette_query, set_palette_query = Bonsai.Cont.state ~equal:[%equal: string] "" graph in
  let palette_index, set_palette_index = Bonsai.Cont.state ~equal:[%equal: int] 0 graph in
  let read_counts, set_read_counts =
    Bonsai.Cont.state ~equal:[%equal: (Target.t * int) list] (load_read_counts ()) graph
  in
  let history_status, set_history_status =
    Bonsai.Cont.state
      ~equal:[%equal: (Target.t * History_status.t) list]
      []
      graph
  in
  (* [autofocus]/mount-time hooks are unreliable for an element that's
     conditionally inserted on a state change rather than present from page
     load. This fires an explicit focus effect instead, timed to exactly
     when we open the palette. *)
  let palette_focus = Bonsai_web.Effect.Focus.on_effect () graph in
  let%arr state
  and selected
  and set_selected
  and compose_text
  and set_compose_text
  and panel
  and set_panel
  and palette_open
  and set_palette_open
  and palette_query
  and set_palette_query
  and palette_index
  and set_palette_index
  and read_counts
  and set_read_counts
  and history_status
  and set_history_status
  and palette_focus
  and send_message
  and raw_command
  and whois
  and channel_modes
  and fetch_history
  and search_logs in
  let channel = current_channel state selected in
  (* Switching INTO a channel shows 0 unread for it by definition (see
     [channel_view]); switching AWAY is when we snapshot how many messages
     it had, so that re-entering later knows what's genuinely new.
     Snapshotting on exit (rather than on entry) means messages that arrive
     while a channel is the active one are correctly never counted, even
     though we don't update anything until you leave. *)
  let switch_to (target : Target.t) =
    let mark_old =
      match selected with
      | Some old when not ([%equal: Target.t] old target) ->
        let key_matches t = [%equal: Target.t] t old in
        let new_read_counts =
          (old, message_count state old) :: List.filter read_counts ~f:(fun (t, _) -> not (key_matches t))
        in
        Vdom.Effect.Many [ set_read_counts new_read_counts; Vdom.Effect.of_sync_fun save_read_counts new_read_counts ]
      | _ -> Vdom.Effect.Ignore
    in
    Vdom.Effect.Many [ mark_old; set_selected (Some target) ]
  in
  let all_targets =
    List.concat_map state.State.networks ~f:(fun (n : Network.t) ->
      List.map n.channels ~f:(fun (c : Channel.t) -> { Target.network = n.name; channel = c.name }))
  in
  let cycle_channel ~forward =
    if List.is_empty all_targets
    then Vdom.Effect.Ignore
    else (
      let count = List.length all_targets in
      let next_index =
        match selected with
        | None -> if forward then 0 else count - 1
        | Some cur ->
          (match List.findi all_targets ~f:(fun _ t -> [%equal: Target.t] t cur) with
           | None -> 0
           | Some (i, _) -> if forward then (i + 1) mod count else (i - 1 + count) mod count)
      in
      match List.nth all_targets next_index with
      | Some target -> switch_to target
      | None -> Vdom.Effect.Ignore)
  in
  let on_nick_click network nick =
    Vdom.Effect.Many
      [ set_panel (Panel.User_info { network; nick; status = Whois_loading })
      ; Vdom.Effect.bind (whois network nick) ~f:(fun result ->
          set_panel
            (Panel.User_info
               { network
               ; nick
               ; status =
                   (match result with
                    | Ok w -> Panel.Whois_loaded w
                    | Error e -> Panel.Whois_error (Error.to_string_hum e))
               }))
      ]
  in
  let on_channel_contextmenu network channel =
    Vdom.Effect.Many
      [ set_panel (Panel.Channel_info { network; channel; status = Modes_loading })
      ; Vdom.Effect.bind (channel_modes network channel) ~f:(fun result ->
          set_panel
            (Panel.Channel_info
               { network
               ; channel
               ; status =
                   (match result with
                    | Ok m -> Panel.Modes_loaded m
                    | Error e -> Panel.Modes_error (Error.to_string_hum e))
               }))
      ]
  in
  (* Fires on every scroll of the chat pane; only acts once it's within
     [history_fetch_threshold_px] of the top and no fetch for this channel is
     already loading or exhausted. *)
  let on_chat_scroll =
    Vdom.Attr.on_scroll (fun _ev ->
      match selected with
      | None -> Vdom.Effect.Ignore
      | Some (target : Target.t) ->
        (match List.Assoc.find history_status target ~equal:[%equal: Target.t] with
         | Some _ -> Vdom.Effect.Ignore
         | None ->
           (match Js_of_ocaml.Dom_html.getElementById_opt "chat-scroll" with
            | None -> Vdom.Effect.Ignore
            | Some el ->
              if el##.scrollTop > history_fetch_threshold_px
              then Vdom.Effect.Ignore
              else (
                let old_scroll_height = el##.scrollHeight in
                let mark_loading =
                  set_history_status
                    ((target, History_status.Loading)
                     :: List.filter history_status ~f:(fun (t, _) ->
                       not ([%equal: Target.t] t target)))
                in
                Vdom.Effect.Many
                  [ mark_loading
                  ; Vdom.Effect.bind
                      (fetch_history target.network target.channel)
                      ~f:(fun result ->
                        let count =
                          match result with
                          | Ok n -> n
                          | Error _ -> 0
                        in
                        let cleared =
                          List.filter history_status ~f:(fun (t, _) ->
                            not ([%equal: Target.t] t target))
                        in
                        let new_status =
                          if count = 0
                          then (target, History_status.Exhausted) :: cleared
                          else cleared
                        in
                        Vdom.Effect.Many
                          [ set_history_status new_status
                          ; Vdom.Effect.of_sync_fun
                              (fun () -> preserve_scroll_after_history_load ~old_scroll_height)
                              ()
                          ])
                  ]))))
  in
  (* Slash commands typed into the compose box. [msg]/[notice]/[me] go
     through [send_message] (so they're echoed locally the same way a plain
     message would be); [join]/[nick]/[mode]/... and any other verb we don't
     special-case fall through to a raw line sent as-is -- the resulting
     JOIN/NICK/etc. line comes back from the server like it would for any
     other client and updates state normally. *)
  let handle_compose_submit text =
    if String.is_empty text
    then Vdom.Effect.Ignore
    else if not (String.is_prefix text ~prefix:"/")
    then (
      match selected with
      | None -> Vdom.Effect.Ignore
      | Some target -> send_message target text Message_kind.Chat)
    else (
      let body = String.drop_prefix text 1 in
      let verb, rest =
        match String.lsplit2 body ~on:' ' with
        | Some (a, b) -> String.lowercase a, String.lstrip b
        | None -> String.lowercase body, ""
      in
      let network = Option.map selected ~f:(fun (t : Target.t) -> t.network) in
      let current_channel = Option.map selected ~f:(fun (t : Target.t) -> t.channel) in
      match verb, network with
      | ("msg" | "m"), Some network ->
        (match String.lsplit2 rest ~on:' ' with
         | Some (target, msg) when not (String.is_empty target) ->
           send_message { Target.network; channel = target } msg Message_kind.Chat
         | _ -> Vdom.Effect.Ignore)
      | "notice", Some network ->
        (match String.lsplit2 rest ~on:' ' with
         | Some (target, msg) when not (String.is_empty target) ->
           send_message { Target.network; channel = target } msg Message_kind.Notice
         | _ -> Vdom.Effect.Ignore)
      | "me", Some network ->
        (match current_channel with
         | Some channel -> send_message { Target.network; channel } rest Message_kind.Action
         | None -> Vdom.Effect.Ignore)
      | "query", Some network ->
        let nick = String.strip rest in
        if String.is_empty nick then Vdom.Effect.Ignore else switch_to { Target.network; channel = nick }
      | "whois", Some network ->
        let nick = String.strip rest in
        if String.is_empty nick then Vdom.Effect.Ignore else on_nick_click network nick
      | "part", Some network ->
        (match current_channel with
         | Some channel ->
           let line =
             if String.is_empty rest then sprintf "PART %s" channel else sprintf "PART %s :%s" channel rest
           in
           raw_command network line
         | None -> Vdom.Effect.Ignore)
      | "topic", Some network ->
        (match current_channel with
         | Some channel ->
           let line =
             if String.is_empty rest
             then sprintf "TOPIC %s" channel
             else sprintf "TOPIC %s :%s" channel rest
           in
           raw_command network line
         | None -> Vdom.Effect.Ignore)
      | ("raw" | "quote"), Some network ->
        if String.is_empty rest then Vdom.Effect.Ignore else raw_command network rest
      | "search", _ ->
        let query = String.strip rest in
        if String.is_empty query
        then Vdom.Effect.Ignore
        else
          Vdom.Effect.Many
            [ set_panel (Panel.Search_results { query; status = Panel.Search_loading })
            ; Vdom.Effect.bind (search_logs query) ~f:(fun result ->
                set_panel
                  (Panel.Search_results
                     { query
                     ; status =
                         (match result with
                          | Ok hits -> Panel.Search_loaded hits
                          | Error e -> Panel.Search_error (Error.to_string_hum e))
                     }))
            ]
      | _, Some network ->
        if String.is_empty verb
        then Vdom.Effect.Ignore
        else raw_command network (sprintf "%s %s" (String.uppercase verb) rest)
      | _, None -> Vdom.Effect.Ignore)
  in
  let close_palette =
    Vdom.Effect.Many [ set_palette_open false; set_palette_query ""; set_palette_index 0 ]
  in
  let open_palette =
    Vdom.Effect.Many
      [ set_palette_open true; set_palette_query ""; set_palette_index 0; palette_focus.focus ]
  in
  let filtered_items =
    (Palette.channel_items state @ Palette.command_items)
    |> List.filter ~f:(fun item -> Palette.matches item palette_query)
    |> fun items -> List.take items Palette.max_results
  in
  let run_item (item : Palette.item) =
    match item.action with
    | Palette.Switch target -> Vdom.Effect.Many [ switch_to target; close_palette ]
    | Palette.Prefill text ->
      (* Commands like /search or /join need more typing after the verb, so
         stay open with the verb filled in rather than punting to the main
         compose box -- the whole point is to finish the command without
         leaving the palette. *)
      Vdom.Effect.Many [ set_palette_query text; set_palette_index 0; palette_focus.focus ]
  in
  let keydown_handler (ev : Js_of_ocaml.Dom_html.keyboardEvent Js_of_ocaml.Js.t) =
    let key =
      match Js_of_ocaml.Js.Optdef.to_option ev##.key with
      | Some k -> Js_of_ocaml.Js.to_string k
      | None -> ""
    in
    let meta_held = Js_of_ocaml.Js.to_bool ev##.metaKey in
    let ctrl_held = Js_of_ocaml.Js.to_bool ev##.ctrlKey in
    if (meta_held || ctrl_held) && String.Caseless.equal key "k"
    then (
      Js_of_ocaml.Dom.preventDefault ev;
      if palette_open then close_palette else open_palette)
    else if (not palette_open) && ctrl_held && String.Caseless.equal key "n"
    then (
      Js_of_ocaml.Dom.preventDefault ev;
      cycle_channel ~forward:true)
    else if (not palette_open) && ctrl_held && String.Caseless.equal key "p"
    then (
      Js_of_ocaml.Dom.preventDefault ev;
      cycle_channel ~forward:false)
    else if not palette_open
    then Vdom.Effect.Ignore
    else (
      match key with
      | "Escape" -> close_palette
      | "ArrowDown" ->
        Js_of_ocaml.Dom.preventDefault ev;
        set_palette_index (min (palette_index + 1) (max 0 (List.length filtered_items - 1)))
      | "ArrowUp" ->
        Js_of_ocaml.Dom.preventDefault ev;
        set_palette_index (max (palette_index - 1) 0)
      | ("n" | "N") when ctrl_held ->
        Js_of_ocaml.Dom.preventDefault ev;
        set_palette_index (min (palette_index + 1) (max 0 (List.length filtered_items - 1)))
      | ("p" | "P") when ctrl_held ->
        Js_of_ocaml.Dom.preventDefault ev;
        set_palette_index (max (palette_index - 1) 0)
      | "Enter" ->
        Js_of_ocaml.Dom.preventDefault ev;
        (* A typed-out slash command (e.g. "/search foo bar", filled in by
           selecting "/search" above and then typing the query) runs
           directly, the same as submitting it from the compose box --
           otherwise Enter just picks whatever's highlighted in the list. *)
        if String.is_prefix palette_query ~prefix:"/"
        then Vdom.Effect.Many [ handle_compose_submit palette_query; close_palette ]
        else (
          match List.nth filtered_items palette_index with
          | Some item -> run_item item
          | None -> Vdom.Effect.Ignore)
      | _ -> Vdom.Effect.Ignore)
  in
  current_keydown_handler := keydown_handler;
  Vdom.Node.div
    ~attrs:[ Vdom.Attr.id "container" ]
    ([ left_sidebar ~state ~selected ~on_select:switch_to ~on_contextmenu:on_channel_contextmenu ~read_counts
     ; chat_panel
         ~channel
         ~network:(Option.value_map selected ~default:"" ~f:(fun (t : Target.t) -> t.network))
         ~on_nick_click
         ~compose_text
         ~set_compose_text
         ~on_submit:handle_compose_submit
         ~on_chat_scroll
     ; right_sidebar
         ~network:(Option.value_map selected ~default:"" ~f:(fun (t : Target.t) -> t.network))
         ~on_member_click:on_nick_click
         channel
     ]
     @ render_panel panel ~set_panel ~on_select:switch_to
     @ render_palette
         ~palette_open
         ~palette_query
         ~set_palette_query
         ~palette_index
         ~set_palette_index
         ~filtered_items
         ~close_palette
         ~run_item
         ~focus_attr:palette_focus.attr)
;;
