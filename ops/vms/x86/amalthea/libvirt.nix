{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "amalthea";
  uuid = "93E78429-0063-49AD-A5EB-3A90E110B673";
  memory = 2;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 amalthea.qcow2 40G
  vmdisk = /keep/pools/vmpool/amalthea.qcow2;
}
