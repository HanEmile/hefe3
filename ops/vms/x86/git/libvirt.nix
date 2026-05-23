{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "git";
  uuid = "A81B81CC-EF5E-4716-B0EF-E8AE3AA9CC4A";
  memory = 2;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 git.qcow2 40G
  vmdisk = /keep/pools/vmpool/git.qcow2;
}
