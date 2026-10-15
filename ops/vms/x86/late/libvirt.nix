{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "late";
  uuid = "E21A306B-C8D2-41ED-9686-F1996EBC7B49";
  memory = 1;
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 md.qcow2 40G
  vmdisk = /keep/pools/vmpool/late.qcow2;
}
