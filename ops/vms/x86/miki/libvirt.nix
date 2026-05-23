{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "miki";
  uuid = "ECCDC38E-4801-4936-8943-C8171DC0E4F7";
  memory = 2;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 miki.qcow2 40G
  vmdisk = /keep/pools/vmpool/miki.qcow2;
}
