{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "factorio";
  uuid = "1c8a4ff9-e02b-4a52-9b16-72b8c3b3a4ed";
  memory = 4; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/factorio.qcow2;
}
