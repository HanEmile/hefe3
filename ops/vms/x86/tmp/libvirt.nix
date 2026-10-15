{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "tmp";
  uuid = "3023EE1B-78A1-4B7F-8754-0D3F4520726D";
  memory = 2; # GB
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 tmp.qcow2 20G
  vmdisk = /keep/pools/vmpool/tmp.qcow2;
}
