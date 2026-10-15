{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "naraj";
  uuid = "D8B9C61F-68F9-4E29-99AA-7B7DFF210471";
  memory = 4;
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 naraj.qcow2 40G
  vmdisk = /keep/pools/vmpool/naraj.qcow2;
}
