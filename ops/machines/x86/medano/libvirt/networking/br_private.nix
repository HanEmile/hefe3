{ nixvirt, ... }:
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
        hosts = [
          {
            name = "rou";
            mac = "52:54:00:90:70:2a";
            ip = "192.168.33.2";
          }
          {
            name = "arr";
            mac = "52:54:00:1a:9e:20";
            ip = "192.168.33.3";
          }
        ];
      };
    }
  ];
}
