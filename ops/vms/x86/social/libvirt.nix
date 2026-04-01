{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "social";
  uuid = "6ACA1D5F-3CCF-4A47-BC91-59798ECDB125";
  memory = 2; # GB
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 social.qcow2 40G
  vmdisk = /keep/pools/vmpool/social.qcow2;
}
