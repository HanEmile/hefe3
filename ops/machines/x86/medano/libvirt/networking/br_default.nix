{
  nixvirt,
  hefe,
  lib,
  ...
}:

{
  # * Defining the "default" network here, used by multiple VMs, so I
  #   obviously won't redefine this in every VM
  # * NATting all traffic on the virbr0 bridge with IPs in the
  #   192.168.74.2..254 range
  # * Fix networking (allowed ports du to nftables <-> iptables switch)
  #   https://github.com/AshleyYakeley/NixVirt/issues/91
  networking.firewall.interfaces."virbr0" = {
    allowedUDPPorts = [
      53
      67
      68
    ];
    allowedTCPPorts = [ 53 ];
  };

  virtualisation.libvirt.connections."qemu:///system".networks = [
    {
      # default network, common VMs that are allowed to talk
      active = true;
      restart = null; # only restart on changes
      definition =

        nixvirt.lib.network.writeXML {
          name = "default";
          uuid = "E111D0FE-0D85-4410-B762-99EDB16C4160";
          forward = {
            mode = "nat";
            nat.port.start = 1024;
            nat.port.end = 65535;
          };
          bridge.name = "virbr0";
          ip =
            let
              prefix = "192.168.75.";
            in
            {
              address = "${prefix}1";
              netmask = "255.255.255.0";
              dhcp = {
                range = {
                  start = "${prefix}2";
                  end = "${prefix}254";
                };

                # Assemble a list of {name,mac,ip} attrs for each ip defined
                # in the hefe.ops.ipam.default net
                hosts = builtins.map (x: {
                  name = x.name;
                  mac = x.value.mac;
                  ip = x.value.v4;
                }) (lib.attrsets.attrsToList hefe.ops.ipam.default);
              };
            };
        };
    }
  ];
}
