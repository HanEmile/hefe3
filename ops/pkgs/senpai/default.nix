{ ... }:
{ senpai }:

# Patches upstream senpai (whatever version nixpkgs currently packages) so
# that connection drops/retries no longer write "Connecting to...",
# "Connection failed" and "Connection lost" lines into every buffer's
# backlog (which corrupts scrollback with interleaved local status noise).
# Instead, disconnected networks are shown in red in the buffer list while
# senpai keeps retrying silently in the background (it already backs off up
# to 1 minute between attempts).
#
# This tracks nixpkgs' senpai src/version directly, so bumping nixpkgs
# upgrades senpai as usual; only re-run `nix-build` if the patch fails to
# apply (i.e. upstream touched the same lines) and fix it up then.
senpai.overrideAttrs (old: {
  patches = (old.patches or [ ]) ++ [ ./no-reconnect-spam.patch ];
})
