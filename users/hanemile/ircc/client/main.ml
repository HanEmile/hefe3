open! Core
open! Async_kernel
open! Bonsai_web
open Async_js
open Ircc_common

let process_state_stream ~conn ~state_var =
  let%bind pipe, _ = Rpc.Pipe_rpc.dispatch_exn Protocol.State_stream.t conn () in
  Pipe.iter pipe ~f:(fun state ->
    Bonsai.Cont.Expert.Var.set state_var state;
    Deferred.unit)
;;

let send_message ~conn =
  let dispatch = Rpc.Rpc.dispatch_exn Protocol.Send_message.t conn |> Effect.of_deferred_fun in
  fun (target : Types.Target.t) text (kind : Types.Message_kind.t) ->
    Effect.bind (dispatch (target, text, kind)) ~f:(function
      | Ok () -> Effect.Ignore
      | Error _ -> Effect.Ignore)
;;

let raw_command ~conn =
  let dispatch = Rpc.Rpc.dispatch_exn Protocol.Raw_command.t conn |> Effect.of_deferred_fun in
  fun network line ->
    Effect.bind (dispatch (network, line)) ~f:(function
      | Ok () -> Effect.Ignore
      | Error _ -> Effect.Ignore)
;;

let whois ~conn =
  let dispatch = Rpc.Rpc.dispatch_exn Protocol.Whois.t conn |> Effect.of_deferred_fun in
  fun network nick -> dispatch (network, nick)
;;

let channel_modes ~conn =
  let dispatch = Rpc.Rpc.dispatch_exn Protocol.Channel_modes.t conn |> Effect.of_deferred_fun in
  fun network channel -> dispatch (network, channel)
;;

let fetch_history ~conn =
  let dispatch = Rpc.Rpc.dispatch_exn Protocol.Fetch_history.t conn |> Effect.of_deferred_fun in
  fun network channel -> dispatch (network, channel)
;;

let search_logs ~conn =
  let dispatch = Rpc.Rpc.dispatch_exn Protocol.Search_logs.t conn |> Effect.of_deferred_fun in
  fun query -> dispatch query
;;

(* A genuinely global keydown listener (capture phase, on [window] itself)
   for Cmd+K/Ctrl+K/Ctrl+N/Ctrl+P, instead of a [Vdom.Attr] on some element --
   an element-level listener only fires while focus happens to be inside that
   element's subtree, which is why that approach (tried first) was flaky. *)
let install_global_keydown_listener () =
  let (_ : Js_of_ocaml.Dom.event_listener_id) =
    Js_of_ocaml.Dom_html.addEventListener
      Js_of_ocaml.Dom_html.window
      Js_of_ocaml.Dom_html.Event.keydown
      (Js_of_ocaml.Dom_html.handler (fun ev ->
         (try Vdom.Effect.Expert.handle_non_dom_event_exn (!App.current_keydown_handler ev) with
          | _ -> ());
         (* Firing the Bonsai "focus" effect above happens before Bonsai has
            actually patched the DOM to insert the palette's <input> (that
            patch is scheduled, not synchronous), so asking a hook to focus
            an element that doesn't exist yet is a no-op -- which is why
            Cmd+K alone wasn't reliably landing focus in the search box. A
            single [setTimeout 0] wasn't enough either: Bonsai's own patch
            is scheduled via [requestAnimationFrame], and a 0ms timeout can
            still fire before the next animation frame. Wait two animation
            frames instead (standard "after the next paint has definitely
            happened" trick) before grabbing the real DOM node by id. *)
         let key =
           match Js_of_ocaml.Js.Optdef.to_option ev##.key with
           | Some k -> Js_of_ocaml.Js.to_string k
           | None -> ""
         in
         let mod_held =
           Js_of_ocaml.Js.to_bool ev##.metaKey || Js_of_ocaml.Js.to_bool ev##.ctrlKey
         in
         if mod_held && String.Caseless.equal key "k"
         then (
           let request_animation_frame f =
             ignore
               (Js_of_ocaml.Dom_html.window##requestAnimationFrame
                  (Js_of_ocaml.Js.wrap_callback (fun (_ : float) -> f ())))
           in
           request_animation_frame (fun () ->
             request_animation_frame (fun () ->
               match Js_of_ocaml.Dom_html.getElementById_opt "palette-input" with
               | Some el -> el##focus
               | None -> ())));
         Js_of_ocaml.Js._true))
      (Js_of_ocaml.Js.bool true)
  in
  ()
;;

let run () =
  Async_js.init ();
  install_global_keydown_listener ();
  let%bind conn = Rpc.Connection.client_exn () in
  let state_var = Bonsai.Cont.Expert.Var.create Types.State.empty in
  let send_message = send_message ~conn in
  let raw_command = raw_command ~conn in
  let whois = whois ~conn in
  let channel_modes = channel_modes ~conn in
  let fetch_history = fetch_history ~conn in
  let search_logs = search_logs ~conn in
  Bonsai_web.Start.start
    (App.component
       ~state:(Bonsai.Cont.Expert.Var.value state_var)
       ~send_message:(Bonsai.Cont.return send_message)
       ~raw_command:(Bonsai.Cont.return raw_command)
       ~whois:(Bonsai.Cont.return whois)
       ~channel_modes:(Bonsai.Cont.return channel_modes)
       ~fetch_history:(Bonsai.Cont.return fetch_history)
       ~search_logs:(Bonsai.Cont.return search_logs));
  don't_wait_for (process_state_stream ~conn ~state_var);
  return ()
;;

let () = don't_wait_for (run ())
