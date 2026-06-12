# lernaeus hardware configuration.
#
# Reconciled with the box as installed on 2026-06-10 (AMD Ryzen 5 5600G,
# Samsung 970 EVO Plus 500GB). The ZFS fileSystems below match the pool
# created during install; the ESP UUID is the real one from `blkid`. See
# ./README.md "As-installed" section for the full command history.
#
# Layout (single 512GB NVMe, UEFI):
#   nvme0n1p1  1G   vfat   -> /boot   (ESP, unencrypted)
#   nvme0n1p2  rest ZFS    -> rpool   (native encryption, passphrase)
#
# Datasets:
#   rpool/root -> /
#   rpool/nix  -> /nix   (noatime)
#   rpool/home -> /home
#   rpool/var  -> /var
{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}:

{
  imports = [ (modulesPath + "/installer/scan/not-detected.nix") ];

  # Refined by nixos-generate-config at install time.
  boot.initrd.availableKernelModules = [
    "nvme"
    "xhci_pci"
    "ahci"
    "usbhid"
    "usb_storage"
    "sd_mod"
  ];
  boot.initrd.kernelModules = [ ];
  boot.kernelModules = [ "kvm-amd" ]; # AMD Ryzen 5 5600G
  boot.extraModulePackages = [ ];

  fileSystems."/" = {
    device = "rpool/root";
    fsType = "zfs";
    options = [ "zfsutil" "X-mount.mkdir" ];
    neededForBoot = true;
  };

  fileSystems."/nix" = {
    device = "rpool/nix";
    fsType = "zfs";
    options = [ "zfsutil" "X-mount.mkdir" ];
  };

  fileSystems."/home" = {
    device = "rpool/home";
    fsType = "zfs";
    options = [ "zfsutil" "X-mount.mkdir" ];
  };

  fileSystems."/var" = {
    device = "rpool/var";
    fsType = "zfs";
    options = [ "zfsutil" "X-mount.mkdir" ];
  };

  # ESP (FAT32, label EFI). UUID confirmed via blkid on the installed box.
  fileSystems."/boot" = {
    device = "/dev/disk/by-uuid/FAE2-3785";
    fsType = "vfat";
    options = [ "fmask=0077" "dmask=0077" ];
  };

  # No disk swap device. We use zramSwap (see default.nix) instead of the
  # rpool/swap zvol: the zvol's /dev/zvol/... udev symlink races the
  # scripted-initrd device wait and wedges boot ("waiting for device
  # /dev/zvol/rpool/swap to appear"). zram needs no device and never
  # blocks boot. The leftover rpool/swap zvol is harmless and can be
  # destroyed later (`zfs destroy rpool/swap`).
  swapDevices = [ ];

  networking.useDHCP = lib.mkDefault true;

  nixpkgs.hostPlatform = lib.mkDefault "x86_64-linux";
  hardware.cpu.amd.updateMicrocode = lib.mkDefault config.hardware.enableRedistributableFirmware;
}
