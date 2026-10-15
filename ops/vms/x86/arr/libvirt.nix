{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "arr";
  uuid = "C982D3B3-9388-470E-A350-53823B05E423";
  memory = 6;
  interfaces = [ "virbr1" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 arr.qcow2 40G
  vmdisk = /keep/pools/vmpool/arr.qcow2;
}
