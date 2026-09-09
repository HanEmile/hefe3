{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "pretix";
  uuid = "95BAD2D5-42F1-480E-A712-F645B39C7098";
  memory = 2;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # cd /keep/pools/vmpool && qemu-img create -f qcow2 pretix.qcow2 40G
  vmdisk = /keep/pools/vmpool/pretix.qcow2;
}
