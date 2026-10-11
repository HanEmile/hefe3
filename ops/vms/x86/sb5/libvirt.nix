{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "sb5";
  uuid = "6E5AEA07-BB21-4108-A186-003C028DC569";
  memory = 4; # GB
  vcpu_count = 4;
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 sb5.qcow2 40G
  vmdisk = /keep/pools/vmpool/sb5.qcow2;
}
