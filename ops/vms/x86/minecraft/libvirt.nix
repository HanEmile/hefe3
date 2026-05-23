{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "minecraft";
  uuid = "0d4e5b71-90ed-4f1b-8da5-3b56b7c1c500";
  memory = 4; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/minecraft.qcow2;
}
