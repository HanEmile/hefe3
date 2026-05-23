{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "sb1";
  uuid = "92D8F4CD-F8C4-4B61-AA88-EE349DB7DEF0";
  memory = 2; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/sb1.qcow2;
}
