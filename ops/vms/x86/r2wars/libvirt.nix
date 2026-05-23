{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "r2wars";
  uuid = "5c0a52b8-1c20-46de-9a48-b88e2c89e4f1";
  memory = 2; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/r2wars.qcow2;
}
