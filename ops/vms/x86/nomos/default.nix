{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.nomos;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../../../modules/nomos.nix
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "nomos";
  system.stateVersion = "25.05";

  # /var/lib/ipfs is the retention anchor for anything published through this
  # node (trust anchors, mandates) — losing it turns published content
  # unresolvable. /var/lib/nomos holds the §6.8 trust-anchor state log:
  # evidence of what this verifier saw and when, not a cache.
  vmBackups.paths = [
    "/var/lib/ipfs"
    "/var/lib/nomos"
    "/var/lib/nomos-platform" # appliance state: hosted TAs, operators, drafts
  ];

  services.healthProbes.probes = [
    { name = "nomos"; url = "http://127.0.0.1:3000/"; }
    # The RPC API answers only POST; a 405 on GET still proves the daemon.
    { name = "kubo-api"; url = "http://127.0.0.1:5001/api/v0/version"; expectedStatus = 405; }
    { name = "self"; url = "http://127.0.0.1:9100/metrics"; }
  ];

  services.nomos = {
    enable = true;

    # Bound to localhost: no consumers yet. To serve something on the tailnet,
    # set bind = ":3000" and open the port on the tailscale0 interface.
    #
    # Verification is refused until the administrator Trust Anchor is set:
    #   services.nomos.adminTA = "k51...";   # bare, no /ipns/ prefix

    ipfs = {
      # medano DNATs pubv4:4001 -> 192.168.75.17:4001 (tcp+udp); announce the
      # dialable public address so published anchors are fetchable from the
      # swarm instead of relying on circuit relays.
      announce = [
        "/ip4/95.217.35.60/tcp/4001"
        "/ip4/95.217.35.60/udp/4001/quic-v1"
      ];
    };

    # The appliance WebUI, served publicly as https://nomoi.emile.space
    # through naraj (the upstream-recommended HTTPS termination). Enroll an
    # operator promptly after each fresh install: the bootstrap password
    # defaults to "nomos" until the first NOMOI operator sign-in closes it.
    platform = {
      enable = true;
      bind = "${ipam.v4}:${toString ipam.ports.platform}";
      publicOrigin = "https://nomoi.emile.space";
    };
  };

  networking.firewall.interfaces."enp1s0" = {
    allowedTCPPorts = [
      4001 # kubo swarm (via medano DNAT)
      ipam.ports.platform # appliance WebUI (via naraj proxyPass)
    ];
    allowedUDPPorts = [ 4001 ]; # kubo swarm quic (via medano DNAT)
  };
}
