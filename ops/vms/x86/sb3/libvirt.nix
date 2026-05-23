{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "sb3";
  uuid = "5E87A779-CC7A-4411-8813-BEA57CF0AF06";
  memory = 2; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/sb3.qcow2;
}
