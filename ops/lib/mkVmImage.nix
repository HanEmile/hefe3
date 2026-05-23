# Build a bootable qcow2 disk image for a VM defined in this repo.
#
# Usage (inside ops/nixos.nix):
#   image = (import ./lib/mkVmImage.nix) {
#     inherit pkgs lib;
#     nixos = sources."nixos-25.11";
#     nixosConfig = (nixosFor "<vmname>").config;
#     name = "<vmname>";
#   };
#
# The image uses partition table = legacy (MBR), ext4 root labeled "nixos",
# and grub on /dev/sda. This matches ops/vms/x86/hardware-image.nix, so the
# image boots straight to the configured NixOS system — no manual nixos-install
# step. Push the qcow2 to medano:/keep/pools/vmpool/<name>.qcow2 and define the
# domain in libvirt.nix without `install_vol`.

{ pkgs, lib, nixos, nixosConfig, name }:

let
  makeDiskImage = import (nixos + "/nixos/lib/make-disk-image.nix");
in
makeDiskImage {
  inherit pkgs lib;
  config = nixosConfig;
  diskSize = "auto";
  additionalSpace = "2048M";
  partitionTableType = "legacy";
  fsType = "ext4";
  label = "nixos";
  format = "qcow2";
  baseName = name;
  installBootLoader = true;
}
