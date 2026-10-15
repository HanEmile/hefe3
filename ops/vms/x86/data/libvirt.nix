{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "data";
  uuid = "1DA269B9-6EE5-4814-BF3B-7E6140137B1C";
  memory = 2;
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 data.qcow2 40G
  vmdisk = /keep/pools/vmpool/data.qcow2;
}
