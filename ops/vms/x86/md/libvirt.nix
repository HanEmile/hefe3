{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "md";
  uuid = "01E6DF1F-D743-45E2-BF2F-D60F2F428BC6";
  memory = 4;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 md.qcow2 40G
  vmdisk = /keep/pools/vmpool/md.qcow2;
}
