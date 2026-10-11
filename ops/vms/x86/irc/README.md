# irc

Ergo (public server, `irc.emile.space`) + soju (personal bouncer,
`irc.pinto-pike.ts.net:6698`, tailscale-only) on one VM. See
`default.nix` for the service config. This file covers the client side:
how senpai/irssi/weechat connect, and how soju itself authenticates to
the upstream networks it bounces.

## Networks behind the bouncer

soju multiplexes three upstream networks under one account (`emile`):

| soju network name | upstream              | soju -> upstream auth |
|--------------------|------------------------|------------------------|
| `emilespace`        | `ircs://irc.emile.space:6697` (Ergo, this VM) | SASL PLAIN (password) |
| `libera`             | `ircs://irc.libera.chat:6697` | SASL EXTERNAL (CertFP) |
| `hackint`            | `ircs://irc.hackint.org:6697` | SASL EXTERNAL (CertFP) |

Desired nick on all three: `hanemile`. Downstream clients (senpai,
irssi, weechat) always authenticate *to soju* with SASL PLAIN using the
bouncer account (`emile`) and the bouncer password - that's separate
from, and unrelated to, how soju itself authenticates upstream to each
network (table above).

`emilespace` can't use CertFP: TLS for `irc.emile.space:6697` is
terminated at `naraj` (a separate proxy VM, see the TLS comment in
`default.nix`), which then forwards *plaintext* to Ergo on this VM.
SASL EXTERNAL requires the IRC daemon itself to see the client's TLS
certificate during the handshake - since that handshake terminates at
naraj, not at Ergo, cert auth is structurally impossible here unless
Ergo gets its own TLS listener (bigger change, not done).

## Client config

### senpai

Config: `~/Library/Application Support/senpai/senpai.scfg` (not in the
repo - carries the bouncer password). senpai speaks the
`soju.im/bouncer-networks` extension, so one connection to the bouncer
(`address "irc.pinto-pike.ts.net:6698"`, bare `username "emile"`) shows
all three upstream networks multiplexed automatically. Nothing else to
configure.

### irssi

Config: `~/.irssi/config` (not in the repo - carries the bouncer
password). irssi does **not** support `soju.im/bouncer-networks`, so
each upstream network needs its own chatnet/server pair, selected via
soju's `username/network` SASL convention:

```
chatnets = {
  "soju-emilespace" = {
    type = "IRC";
    nick = "hanemile";
    sasl_mechanism = "PLAIN";
    sasl_username = "emile/emilespace";
    sasl_password = "<bouncer password>";
  };
  # ...same pattern for soju-libera, soju-hackint
};

servers = (
  {
    address = "irc.pinto-pike.ts.net";
    chatnet = "soju-emilespace";
    port = "6698";
    use_tls = "yes";
    tls_verify = "yes";
    autoconnect = "yes";
  },
  # ...same pattern for soju-libera, soju-hackint
);
```

Connecting with the bare username (`emile`, no `/network` suffix) lands
on the bouncer's control connection instead of a network - fine for
`BouncerServ` commands, useless for chatting (`JOIN`/`WHOIS`/etc. are
rejected there). This is also why a single "just works" irssi
connection isn't possible - it needs all three server blocks above with
`autoconnect = "yes"` to come up fully connected on launch.

A downstream client cert (SASL EXTERNAL irssi -> soju, so the bouncer
password doesn't sit in plaintext in `~/.irssi/config`) was explored and
reverted - see "Downstream client-cert auth" below.

### weechat

No declarative home-manager module for weechat (unlike irssi) - the
package is installed via `home.packages` and the servers are configured
directly in weechat's own config (`~/.config/weechat/irc.conf`, not in
the repo). Same per-network pattern as irssi, set via `/set` (don't
hand-edit `irc.conf` - weechat overwrites it on `/save`/quit):

```
/server add soju-emilespace irc.pinto-pike.ts.net/6698 -tls
/set irc.server.soju-emilespace.tls_verify on
/set irc.server.soju-emilespace.sasl_mechanism plain
/set irc.server.soju-emilespace.sasl_username emile/emilespace
/set irc.server.soju-emilespace.sasl_password <bouncer password>
/set irc.server.soju-emilespace.nicks hanemile
/set irc.server.soju-emilespace.autoconnect on
/set irc.server.soju-emilespace.autojoin #general
# ...same pattern for soju-libera, soju-hackint (no autojoin set for those)
/save
```

Can be scripted non-interactively: `weechat-headless --run-command
"<cmds separated by ;>"`.

## soju's upstream auth (soju -> network)

Each network entry in soju can authenticate upstream via `-pass`
(SASL PLAIN) or a generated client cert (SASL EXTERNAL / CertFP).
Manage networks via `BouncerServ` (PM it after connecting to the
bouncer with any client):

```
/network update <name> -username <user> -pass <pass>
/network status
/sasl status [-network <name>]
```

**Important**: `/msg BouncerServ certfp generate -network <name>`
*immediately* flips that network's stored SASL mechanism to EXTERNAL as
a side effect - `-pass` alone never reverts it, and there's no
BouncerServ command to switch the mechanism back to PLAIN other than
`sasl set-plain` (below). Don't run `certfp generate` on a network
you're not ready to finish setting up.

### Switching a network to SASL PLAIN

```
/msg BouncerServ sasl set-plain -network <name> <username> <password>
```

This only touches the SASL PLAIN fields and the mechanism flag - it
does **not** clear a previously-generated cert, so you can flip back to
PLAIN, do something while logged in (e.g. register a fresh cert's
fingerprint), then let a later `certfp generate` flip back to EXTERNAL
without losing anything.

### Generating + registering a client cert (CertFP)

```
/msg BouncerServ certfp generate -key-type ed25519 -network <name>
```

soju generates the keypair and holds the private key itself - it never
leaves the server. This also sets the network's mechanism to EXTERNAL
immediately (see warning above). It prints SHA-1/256/512 fingerprints;
**which one to register depends on the network**:

- Ergo, hackint (atheme): SHA-256, via `/msg NickServ CERT ADD <fingerprint>`
- libera (atheme, but policy-restricted): **SHA-512 only** - NickServ
  will tell you if you use the wrong one
  (`Fingerprints on this network must be SHA2-512 digests...`)

`CERT ADD` requires being logged in first. If the network just flipped
to EXTERNAL with an unregistered cert, you won't be logged in on
reconnect and `IDENTIFY` may not even exist (hackint's NickServ has no
`IDENTIFY` command at all - login only happens via SASL at connect
time). The reliable sequence that avoids ever needing a raw DB edit:

1. `sasl set-plain -network <name> <user> <pass>` + reconnect (`-enabled
   false` then `-enabled true`) -> logs in via password automatically.
2. `certfp generate -network <name>` -> new cert, mechanism flips to
   EXTERNAL, but the *already-live* PLAIN-authenticated session from
   step 1 is untouched.
3. On that still-live session: `/msg NickServ CERT ADD <fingerprint>`
   (explicit argument - don't rely on auto-detection from the live
   connection; that session never presented a TLS client cert since it
   connected under the old PLAIN mechanism).
4. Reconnect again (`-enabled false`/`true`) -> now actually uses
   EXTERNAL with a registered cert. Confirm with `/sasl status -network
   <name>`.

### Nick auto-regain

Each network has `-nick hanemile`. soju has its own built-in nick-regain
backoff (retries the desired nick periodically on its own). If a stale
session is holding the nick and you're logged in (SASL PLAIN or
EXTERNAL both count), you can also force it immediately as a regular
client:

```
/msg NickServ GHOST hanemile
/nick hanemile
```

**Rate-limit note**: libera and hackint both have abuse heuristics that
react to rapid repeated connections/nick changes from the same source
(observed: hackint returned `437 temporarily unavailable` on the nick
for ~20-30 minutes, libera fell back to a server-assigned `GuestNNNN`
nick). Don't script rapid-fire reconnect/WHOIS/GHOST loops against them
- space out test connections and let soju's own backoff do the
retrying; a single clean `-enabled false`/`true` toggle is fine once
things have settled.

### Downstream client-cert auth (irssi/weechat -> soju): not currently usable

soju 0.10.1 (what nixpkgs packaged until this was bumped) predates the
`client-cert-auth` config directive (added upstream 2026-03-12), so
enabling it crashed soju outright (`unknown directive`). This is why
`ops/pkgs/soju/default.nix` tracks the latest upstream tag directly via
an overlay (see `ops/nixos.nix`) - 0.11.1 does support the directive.
Re-enabling `services.soju.extraConfig = "client-cert-auth true";` is
the next step if you want to pursue this (it still needs `BouncerServ
device-certificate create` to associate a cert with the account -
fingerprint-keyed, independent of username/network, see
`downstream.go`'s `EXTERNAL` SASL case upstream). Not done because the
bouncer password sitting in `~/.irssi/config`/weechat's `irc.conf` is a
much smaller risk than it first seemed, given both files are
machine-local and outside the repo.

## Files referenced

- `ops/vms/x86/irc/default.nix` - Ergo + soju NixOS service config.
- `ops/pkgs/soju/default.nix` - soju package override (tracks upstream
  tags; nixpkgs lags).
- `~/Library/Application Support/senpai/senpai.scfg` - senpai config
  (local, not in repo).
- `~/.irssi/config` - irssi config (local, not in repo).
- `~/.config/weechat/irc.conf` - weechat config (local, not in repo).
