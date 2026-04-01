{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "ctf";
  uuid = "D813CDA4-B5C6-4761-8D0A-D921083DA7CD";
  vcpu_count = 10;
  memory = 32;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 ctf.qcow2 40G
  vmdisk = /keep/pools/vmpool/ctf.qcow2;
}
