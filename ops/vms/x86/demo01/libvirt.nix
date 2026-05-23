{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "demo01";
  uuid = "26B0F0F5-1A5B-4D14-A4F6-AF1A8E8C0C01";
  memory = 1; # GB — minimal test VM
  interfaces = [ "virbr0" ];

  # NB: no install_vol. The disk image is built by ops.nixos.demo01.deploy_image
  # and is bootable directly. Run:
  #   nix-build -A ops.nixos.demo01.deploy_image && ./result/bin/deploy-image
  # before first start.
  vmdisk = /keep/pools/vmpool/demo01.qcow2;
}
