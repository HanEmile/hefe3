{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "rss";
  uuid = "8ea36421-744f-47a9-9cf8-3dcb48b403b2";
  memory = 2; # GB
  interfaces = [ "virbr0" ];

  # comment out after first install
  install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 rss.qcow2 40G
  vmdisk = /keep/pools/vmpool/rss.qcow2;
}
