{ nixvirt, hefe, lib, ... }:
let
  def = import ./def.nix { inherit nixvirt; };
in
{
  # * Fix networking (allowed ports du to nftables <-> iptables switch)
  #   https://github.com/AshleyYakeley/NixVirt/issues/91
  networking.firewall.interfaces."virbr1" = {
    allowedUDPPorts = [
      53
      67
      68
    ];
    allowedTCPPorts = [ 53 ];
  };

  virtualisation.libvirt.connections."qemu:///system".networks = [
    {
      # private network
      active = true;
      restart = null; # only restart on changes
      definition = def {
        name = "private";
        uuid = "934EFD44-3EEB-4E27-9EAA-3E0E0977FAAF";
        forward = null;
        bridgename = "virbr1";
        prefix = "192.168.33.";
        # DHCP reservations come from the ipam "private" namespace, same as
        # br_default/br_code do for their bridges.
        hosts = builtins.map (x: {
          name = x.name;
          mac = x.value.mac;
          ip = x.value.v4;
        }) (lib.attrsets.attrsToList hefe.ops.ipam.private);
      };
    }
  ];
}
