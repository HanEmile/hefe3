{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "photo";
  uuid = "B50A888B-1B77-4D46-A1AA-D25F7E72C675";
  memory = 4; # GB
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 photo.qcow2 40G
  vmdisk = /keep/pools/vmpool/photo.qcow2;
}
