{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "arr";
  uuid = "C982D3B3-9388-470E-A350-53823B05E423";
  memory = 8;
  interfaces = [ "virbr1" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 arr.qcow2 40G
  vmdisk = /keep/pools/vmpool/arr.qcow2;
}
