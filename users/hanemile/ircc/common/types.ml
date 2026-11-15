open! Core

(** Shared state types, pushed from server to client as one snapshot per
    change (see [Protocol.State_stream]). Simple but chatty; fine for a
    single personal user. *)

module Member = struct
  type t =
    { nick : string
    ; op : bool
    ; voice : bool
    }
  [@@deriving sexp, bin_io, equal, compare]
end

module Message_kind = struct
  type t =
    | Chat
    | Notice
    | Action (* /me, or an incoming CTCP ACTION *)
    | Join
    | Part
    | Quit
    | Nick_change
    | Status
  [@@deriving sexp, bin_io, equal, compare]
end

module Message = struct
  type t =
    { date : string (* yyyy-mm-dd *)
    ; time : string (* hh:mm *)
    ; nick : string
    ; text : string
    ; kind : Message_kind.t
    ; time_tag : string option
      (* Raw ISO8601 server-time tag (e.g. "2026-10-05T10:38:54.000Z"), kept
         around so the server can anchor a CHATHISTORY BEFORE request off the
         oldest loaded message. [None] for messages that arrived without one,
         e.g. locally-echoed outgoing messages. *)
    }
  [@@deriving sexp, bin_io, equal, compare]
end

module Channel = struct
  type t =
    { name : string
    ; topic : string option
    ; members : Member.t list
    ; messages : Message.t list
    }
  [@@deriving sexp, bin_io, equal, compare]
end

module Network = struct
  type t =
    { name : string
    ; connected : bool
    ; channels : Channel.t list
    }
  [@@deriving sexp, bin_io, equal, compare]
end

module State = struct
  type t =
    { my_nick : string (* so the client can tell when it's been mentioned *)
    ; networks : Network.t list
    }
  [@@deriving sexp, bin_io, equal, compare]

  let empty = { my_nick = ""; networks = [] }
end

module Target = struct
  type t =
    { network : string
    ; channel : string
    }
  [@@deriving sexp, bin_io, equal, compare]
end

module Log_hit = struct
  type t =
    { network : string
    ; channel : string
    ; date : string (* yyyy-mm-dd *)
    ; time : string (* hh:mm:ss, from the log line itself *)
    ; nick : string
    ; text : string
    ; kind : Message_kind.t
    }
  [@@deriving sexp, bin_io, equal, compare]
end

module Whois_result = struct
  type t =
    { nick : string
    ; username : string option
    ; host : string option
    ; realname : string option
    ; server : string option
    ; account : string option
    ; away : string option
    ; channels : string list
    }
  [@@deriving sexp, bin_io, equal, compare]
end
