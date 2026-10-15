{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "auth";
  uuid = "6C388678-FF72-42A4-9F6B-68D327E14F94";
  memory = 2;
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 auth.qcow2 40G
  vmdisk = /keep/pools/vmpool/auth.qcow2;
}
