{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "uptimebot";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts fleet-wide up/down transitions to an IRC channel";
    longDescription = ''
      Runs a list of checks (node-exporter + healthProbes metrics relay over
      tailscale, ICMP ping, or raw TCP connect) against a JSON target list,
      diffs the result against the last-known state, and posts only the
      transitions (up->down, down->up) to an IRC channel via a minimal
      raw-protocol connect/join/privmsg/quit. Meant to be run periodically
      from a systemd timer.
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/uptimebot";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "uptimebot";
  };
})
