{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "sb4";
  uuid = "CC7B292C-F4E1-4127-A809-D6E72D439BD8";
  memory = 4; # GB
  vcpu_count = 4;
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 sb4.qcow2 40G
  vmdisk = /keep/pools/vmpool/sb4.qcow2;
}
