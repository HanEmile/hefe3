# Generic hardware-configuration for VMs whose disk image is built via
# ops.lib.mkVmImage (make-disk-image, partitionTableType = "legacy",
# fsType = "ext4", label = "nixos").
#
# Replaces the per-VM hardware-configuration.nix that nixos-generate-config
# would otherwise produce. By using a label-based mount, every image-built
# VM uses the same disk layout — no per-VM UUIDs.
{ config, lib, pkgs, modulesPath, ... }:

{
  imports = [
    (modulesPath + "/profiles/qemu-guest.nix")
  ];

  boot.initrd.availableKernelModules = [ "ahci" "xhci_pci" "sd_mod" "sr_mod" "virtio_pci" "virtio_blk" ];
  boot.initrd.kernelModules = [ ];
  boot.kernelModules = [ "kvm-intel" ];
  boot.extraModulePackages = [ ];

  boot.loader.grub = {
    enable = true;
    device = "/dev/sda";
    efiSupport = false;
  };

  fileSystems."/" = {
    device = "/dev/disk/by-label/nixos";
    fsType = "ext4";
  };

  swapDevices = [ ];

  # vm-base.nix sets the static network from IPAM; do NOT enable DHCP here.
  networking.useDHCP = lib.mkDefault false;

  nixpkgs.hostPlatform = lib.mkDefault "x86_64-linux";
}
