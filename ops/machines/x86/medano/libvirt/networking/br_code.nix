{
  nixvirt,
  hefe,
  lib,
  pkgs,
  ...
}:

let
  def = import ./def.nix { inherit nixvirt; };
in
{
  # * Fix networking (allowed ports du to nftables <-> iptables switch)
  #   https://github.com/AshleyYakeley/NixVirt/issues/91
  networking.firewall.interfaces."virbr3" = {
    allowedUDPPorts = [
      53
      67
      68
    ];
    allowedTCPPorts = [ 53 ];
  };

  virtualisation.libvirt.connections."qemu:///system".networks = [
    {
      # code network: sandboxed VMs running (untrusted) coding agents.
      # NAT gives them outbound internet (repos, packages, APIs) while the
      # firewall rules below cut them off from the other VM bridges and
      # from services on medano itself (no per-interface INPUT rules here
      # besides libvirt's own DHCP/DNS, so the nixos default-refuse applies).
      active = true;
      restart = null; # only restart on changes
      definition = def {
        name = "code";
        uuid = "937F6CE9-6B61-422B-9BEC-4B958FB2D2E3";
        forward = {
          mode = "nat";
          nat.port.start = 1024;
          nat.port.end = 65535;
        };
        bridgename = "virbr3";
        prefix = "192.168.35.";
        hosts = builtins.map (x: {
          name = x.name;
          mac = x.value.mac;
          ip = x.value.v4;
        }) (lib.attrsets.attrsToList hefe.ops.ipam.code);
      };
    }
  ];

  # Isolation: drop east-west forwarding from virbr3 into the other bridges.
  # Inserted at the top of FORWARD so they sit above libvirt's LIBVIRT_FWD
  # accepts (which would otherwise happily forward virbr3 traffic anywhere).
  networking.firewall.extraCommands = ''
    # code (virbr3) isolation: no east-west into the other VM bridges
    iptables -I FORWARD 1 -i virbr3 -o virbr0 -j DROP
    iptables -I FORWARD 1 -i virbr3 -o virbr1 -j DROP
    iptables -I FORWARD 1 -i virbr3 -o virbr2 -j DROP
  '';

  # Same LIBVIRT_FWI gotcha as medano-forward-fixup (see
  # medano/networking.nix): libvirt re-inserts its chains at `-I FORWARD 1`
  # when (re)starting, which would shove our DROPs below its blanket
  # accepts. PartOf=libvirtd.service re-asserts the rules whenever libvirtd
  # restarts; each insert is preceded by a tolerant delete, so it's
  # idempotent.
  systemd.services.medano-code-isolation-fixup = {
    description = "Re-assert virbr3 isolation DROPs above libvirt's chains";
    wantedBy = [
      "multi-user.target"
      "libvirtd.service"
    ];
    after = [
      "firewall.service"
      "libvirtd.service"
    ];
    partOf = [ "libvirtd.service" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      ExecStart = pkgs.writeShellScript "medano-code-isolation-fixup" ''
        set -eu
        IPT=${pkgs.iptables}/bin/iptables
        for br in virbr0 virbr1 virbr2; do
          "$IPT" -D FORWARD -i virbr3 -o "$br" -j DROP 2>/dev/null || true
        done
        for br in virbr0 virbr1 virbr2; do
          "$IPT" -I FORWARD 1 -i virbr3 -o "$br" -j DROP
        done
      '';
    };
  };
}
