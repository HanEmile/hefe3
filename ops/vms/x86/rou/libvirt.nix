{ nixvirt, ... }:


import ../libvirt-base.nix { inherit nixvirt; } {
  name = "rou";
  uuid = "12E40ADD-2BCA-4393-B027-C241632A88F3";
  memory = 1;
  interfaces = [ "virbr1" "virbr2" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 rou.qcow2 40G
  vmdisk = /keep/pools/vmpool/rou.qcow2;
}
