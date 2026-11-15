open! Core
open! Async_rpc_kernel

(** One pipe that re-sends the entire [State.t] snapshot every time anything
    changes (new message, join/part, mode change, ...), plus one RPC to send
    a message. Deliberately not incremental: the state is small (one user's
    worth of channels/history) so resending it all is simpler than diffing,
    and still fine for a personal client. *)

module State_stream = struct
  let t =
    Rpc.Pipe_rpc.create
      ~name:"state_stream"
      ~version:0
      ~bin_query:[%bin_type_class: unit]
      ~bin_response:[%bin_type_class: Types.State.t]
      ~bin_error:[%bin_type_class: unit]
      ()
  ;;
end

module Send_message = struct
  let t =
    Rpc.Rpc.create
      ~name:"send_message"
      ~version:0
      ~bin_query:[%bin_type_class: Types.Target.t * string * Types.Message_kind.t]
      ~bin_response:[%bin_type_class: unit Or_error.t]
      ~include_in_error_count:Or_error
  ;;
end

(** (network, raw IRC line without CRLF) -- an escape hatch for slash
    commands (/join, /nick, /part, /topic, /quote, ...) that don't fit the
    send-a-message-to-a-target shape. The server doesn't interpret the line
    at all; whatever state change results (a JOIN, a topic change, ...)
    comes back through the normal line parser like it would from any other
    client, so it still ends up in [State.t]. *)
module Raw_command = struct
  let t =
    Rpc.Rpc.create
      ~name:"raw_command"
      ~version:0
      ~bin_query:[%bin_type_class: string * string]
      ~bin_response:[%bin_type_class: unit Or_error.t]
      ~include_in_error_count:Or_error
  ;;
end

(** (network, nick) -> WHOIS result *)
module Whois = struct
  let t =
    Rpc.Rpc.create
      ~name:"whois"
      ~version:0
      ~bin_query:[%bin_type_class: string * string]
      ~bin_response:[%bin_type_class: Types.Whois_result.t Or_error.t]
      ~include_in_error_count:Or_error
  ;;
end

(** (network, channel) -> raw channel mode string, e.g. "+ntC" *)
module Channel_modes = struct
  let t =
    Rpc.Rpc.create
      ~name:"channel_modes"
      ~version:0
      ~bin_query:[%bin_type_class: string * string]
      ~bin_response:[%bin_type_class: string Or_error.t]
      ~include_in_error_count:Or_error
  ;;
end

(** Full-text search over soju's on-disk logs for every network/channel this
    bouncer account has (not just what's currently loaded in [State.t]), most
    recent match first. *)
module Search_logs = struct
  let t =
    Rpc.Rpc.create
      ~name:"search_logs"
      ~version:0
      ~bin_query:[%bin_type_class: string]
      ~bin_response:[%bin_type_class: Types.Log_hit.t list Or_error.t]
      ~include_in_error_count:Or_error
  ;;
end

(** (network, channel) -> number of older messages fetched and prepended to
    that channel's history (via CHATHISTORY BEFORE against the bouncer). The
    messages themselves arrive through [State_stream] like any other state
    change; this RPC's response just tells the client whether it got a full
    page back (keep allowing more loads) or fewer than requested / zero
    (there's no more backlog, stop trying). *)
module Fetch_history = struct
  let t =
    Rpc.Rpc.create
      ~name:"fetch_history"
      ~version:0
      ~bin_query:[%bin_type_class: string * string]
      ~bin_response:[%bin_type_class: int Or_error.t]
      ~include_in_error_count:Or_error
  ;;
end
