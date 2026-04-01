{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "irc";
  uuid = "7F748F88-810F-462F-8C93-1213F1609FF8";
  memory = 4;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 irc.qcow2 40G
  vmdisk = /keep/pools/vmpool/irc.qcow2;
}
