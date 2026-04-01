{ nixvirt, ... }:

{
  name,
  uuid,
  forward,
  bridgename,
  prefix,
  hosts,
}:
with nixvirt.lib.network;
writeXML {
  inherit name uuid forward;
  bridge.name = bridgename;
  ip =
    {
      address = "${prefix}1";
      netmask = "255.255.255.0";
      dhcp = {
        range = {
          start = "${prefix}2";
          end = "${prefix}254";
        };
        inherit hosts;
      };
    };
}
