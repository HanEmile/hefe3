{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "sb2";
  uuid = "5E3ACB0D-410D-4CBF-90B8-78B7B0F4F816";
  memory = 2; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/sb2.qcow2;
}
