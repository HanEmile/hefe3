{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "social";
  uuid = "6ACA1D5F-3CCF-4A47-BC91-59798ECDB125";
  memory = 1; # GB
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 social.qcow2 40G
  vmdisk = /keep/pools/vmpool/social.qcow2;
}
