{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "nomos";
  uuid = "AE52A677-D975-4D6A-88E1-0DC95BC1CBF2";
  memory = 2; # GB - kubo sits next to the verifier
  interfaces = [ "virbr0" ];

  # NB: no install_vol. The disk image is built by ops.nixos.nomos.deploy_image
  # and is bootable directly. Run:
  #   nix-build -A ops.nixos.nomos.deploy_image && ./result/bin/deploy-image
  # before first start.
  vmdisk = /keep/pools/vmpool/nomos.qcow2;
}
