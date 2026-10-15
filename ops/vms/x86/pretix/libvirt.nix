{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "pretix";
  uuid = "95BAD2D5-42F1-480E-A712-F645B39C7098";
  memory = 2;
  interfaces = [ "virbr0" ];

  # cd /keep/pools/vmpool && qemu-img create -f qcow2 pretix.qcow2 40G
  vmdisk = /keep/pools/vmpool/pretix.qcow2;
}
