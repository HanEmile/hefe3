{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "data";
  uuid = "1DA269B9-6EE5-4814-BF3B-7E6140137B1C";
  memory = 4;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 data.qcow2 40G
  vmdisk = /keep/pools/vmpool/data.qcow2;
}
