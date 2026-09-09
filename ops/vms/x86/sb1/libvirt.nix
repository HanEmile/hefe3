{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "sb1";
  uuid = "92D8F4CD-F8C4-4B61-AA88-EE349DB7DEF0";
  memory = 8; # GB (bumped from 2 for fuzzing)
  vcpu_count = 8; # (bumped from 4 for fuzzing)
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/sb1.qcow2;
}
