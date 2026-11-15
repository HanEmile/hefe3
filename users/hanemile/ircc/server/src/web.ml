open! Core
open! Async
open Ircc_common

(* No caching at all for the HTML/JS/CSS: this is a single-user tool that
   gets redeployed often, and a browser silently serving a stale main.js/
   style.css after a deploy (no cache-busting URL, no validators) wasted a
   lot of debugging time on changes that looked like they weren't working. *)
let respond_string ~content_type ?flush ?headers ?status s =
  let headers =
    Cohttp.Header.add_opt headers "Content-Type" content_type
    |> fun h -> Cohttp.Header.add h "Cache-Control" "no-cache, no-store, must-revalidate"
  in
  Cohttp_async.Server.respond_string ?flush ~headers ?status s
;;

let not_found_html =
  {|<!DOCTYPE html><html><head><title>404</title></head><body><h1>404 Not Found</h1></body></html>|}
;;

let html =
  {|<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <script defer src="main.js"></script>
    <link rel="stylesheet" type="text/css" href="style.css">
    <title>ircc</title>
  </head>
  <body>
    <div id="app"></div>
  </body>
</html>
|}
;;

let handler ~body:_ _inet req =
  match Uri.path (Cohttp.Request.uri req) with
  | "" | "/" | "/index.html" -> respond_string ~content_type:"text/html" html
  | "/main.js" ->
    respond_string ~content_type:"application/javascript" Embedded_files.main_dot_bc_dot_js
  | "/style.css" -> respond_string ~content_type:"text/css" Embedded_files.style_dot_css
  | _ -> respond_string ~content_type:"text/html" ~status:`Not_found not_found_html
;;

let implementations (global_state : Irc.Global_state.t) ~(irc_config : Irc.Config.t) =
  let nick = irc_config.nick in
  let state_stream =
    let f _conn_state () =
      Irc.Global_state.subscribe global_state |> Deferred.Result.return
    in
    Async_rpc_kernel.Rpc.Pipe_rpc.implement Protocol.State_stream.t f
  in
  let send_message =
    let f _conn_state (target, text, kind) = Irc.send_message global_state ~nick ~target ~text ~kind in
    Async_rpc_kernel.Rpc.Rpc.implement Protocol.Send_message.t f
  in
  let raw_command =
    let f _conn_state (network, line) = Irc.do_raw_command ~network ~line in
    Async_rpc_kernel.Rpc.Rpc.implement Protocol.Raw_command.t f
  in
  let whois =
    let f _conn_state (network, nick) = Irc.do_whois ~network ~nick in
    Async_rpc_kernel.Rpc.Rpc.implement Protocol.Whois.t f
  in
  let channel_modes =
    let f _conn_state (network, channel) = Irc.do_channel_modes ~network ~channel in
    Async_rpc_kernel.Rpc.Rpc.implement Protocol.Channel_modes.t f
  in
  let fetch_history =
    let f _conn_state (network, channel) = Irc.do_fetch_history global_state ~network ~channel in
    Async_rpc_kernel.Rpc.Rpc.implement Protocol.Fetch_history.t f
  in
  let search_logs =
    let f _conn_state query =
      Irc.do_search_logs
        ~logs_dir:irc_config.logs_dir
        ~bouncer_user:irc_config.bouncer_user
        ~query
    in
    Async_rpc_kernel.Rpc.Rpc.implement Protocol.Search_logs.t f
  in
  Async_rpc_kernel.Rpc.Implementations.create_exn
    ~implementations:
      [ state_stream
      ; send_message
      ; raw_command
      ; whois
      ; channel_modes
      ; fetch_history
      ; search_logs
      ]
    ~on_unknown_rpc:`Continue
;;

let main ~port ~(irc_config : Irc.Config.t) =
  let global_state = Irc.Global_state.create () in
  Irc.Global_state.update global_state ~f:(fun s ->
    { s with Ircc_common.Types.State.my_nick = irc_config.nick });
  Irc.run global_state irc_config;
  let hostname = Core_unix.gethostname () in
  printf "Serving http://%s:%d/\n%!" hostname port;
  let%bind server =
    Rpc_websocket.Rpc.serve
      ~on_handler_error:`Ignore
      ~mode:`TCP
      ~where_to_listen:(Tcp.Where_to_listen.of_port port)
      ~http_handler:(fun () -> handler)
      ~implementations:(implementations global_state ~irc_config)
      ~initial_connection_state:(fun _initiated_from _addr _inet (_connection : Async_rpc_kernel.Rpc.Connection.t) -> ())
      ()
  in
  Cohttp_async.Server.close_finished server
;;
