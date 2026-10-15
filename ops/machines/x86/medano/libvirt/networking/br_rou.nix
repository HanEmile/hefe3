{ nixvirt, hefe, lib, ... }:
let
  def = import ./def.nix { inherit nixvirt; };
in
{
  # * Fix networking (allowed ports du to nftables <-> iptables switch)
  #   https://github.com/AshleyYakeley/NixVirt/issues/91
  networking.firewall.interfaces."virbr2" = {
    allowedUDPPorts = [
      53
      67
      68
    ];
    allowedTCPPorts = [ 53 ];
  };

  virtualisation.libvirt.connections."qemu:///system".networks = [
    {
      # rou network
      active = true;
      restart = null; # only restart on changes
      definition = def {
        name = "rou";
        uuid = "DD420B69-3A68-46F7-AA8D-9E19A783C51D";
        forward = {
          mode = "nat";
          nat.port.start = 1024;
          nat.port.end = 65535;
        };
        bridgename = "virbr2";
        prefix = "192.168.34.";
        # DHCP reservations come from the ipam "rou" namespace, same as
        # br_default/br_code do for their bridges.
        hosts = builtins.map (x: {
          name = x.name;
          mac = x.value.mac;
          ip = x.value.v4;
        }) (lib.attrsets.attrsToList hefe.ops.ipam.rou);
      };
    }
  ]; # end of networks
}
