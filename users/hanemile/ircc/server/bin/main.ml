open! Core
open! Async

let command =
  Command.async
    ~summary:"ircc: a tiny web IRC client for a personal soju bouncer"
    (let%map_open.Command port =
       flag "port" (optional_with_default 8080 int) ~doc:"PORT serve the web UI on PORT"
     and host =
       flag "bouncer-host" (required string) ~doc:"HOST soju bouncer hostname"
     and bouncer_port =
       flag "bouncer-port" (optional_with_default 6697 int) ~doc:"PORT soju bouncer TLS port"
     and bouncer_user =
       flag "bouncer-user" (required string) ~doc:"USER soju bouncer account username"
     and bouncer_pass =
       flag
         "bouncer-pass-file"
         (required Filename_unix.arg_type)
         ~doc:"FILE file containing the soju bouncer account password"
     and nick = flag "nick" (required string) ~doc:"NICK desired IRC nickname"
     and networks =
       flag
         "network"
         (listed string)
         ~doc:"NAME soju upstream network name (repeatable)"
     and logs_dir =
       flag
         "logs-dir"
         (optional_with_default "/var/lib/soju/logs" string)
         ~doc:"DIR root of soju's on-disk message logs, for /search"
     in
     fun () ->
       let%bind bouncer_pass =
         let%map contents = Reader.file_contents bouncer_pass in
         String.strip contents
       in
       let irc_config =
         { Ircc_server.Irc.Config.host
         ; port = bouncer_port
         ; bouncer_user
         ; bouncer_pass
         ; nick
         ; networks
         ; logs_dir
         }
       in
       Ircc_server.Web.main ~port ~irc_config)
    ~behave_nicely_in_pipeline:false
;;

let () = Command_unix.run command
