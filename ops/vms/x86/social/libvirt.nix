{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "social";
  uuid = "6ACA1D5F-3CCF-4A47-BC91-59798ECDB125";
  memory = 2; # GB - gotosocial (sqlite + wasm + federation) hit oom-kill 5x in 7 days at 1GB (2026-11-16)
  interfaces = [ "virbr0" ];

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 social.qcow2 40G
  vmdisk = /keep/pools/vmpool/social.qcow2;
}
