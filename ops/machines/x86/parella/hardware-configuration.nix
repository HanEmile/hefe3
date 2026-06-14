# Parella hardware configuration.
#
# B450 Gaming-ITX/ac, AMD Ryzen 5 2600, 8GB DDR4-2133, 250GB WD Blue
# NVMe SSD, Intel Xeon Phi 3120P (PCIe).
#
# Layout (single 250GB NVMe, UEFI):
#   nvme0n1p1  1G   vfat  -> /boot  (ESP, unencrypted)
#   nvme0n1p2  rest ZFS   -> rpool  (native encryption, aes-256-gcm)
#
# Datasets:
#   rpool/root -> /
#   rpool/nix  -> /nix
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

  boot.initrd.availableKernelModules = [
    "nvme"
    "xhci_pci"
    "ahci"
    "usbhid"
    "usb_storage"
    "sd_mod"
  ];
  boot.initrd.kernelModules = [ ];
  boot.kernelModules = [ "kvm-amd" ];
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

  fileSystems."/boot" = {
    device = "/dev/disk/by-uuid/B045-D514";
    fsType = "vfat";
    options = [ "fmask=0077" "dmask=0077" ];
  };

  swapDevices = [ ];

  networking.useDHCP = lib.mkDefault true;

  nixpkgs.hostPlatform = lib.mkDefault "x86_64-linux";
  hardware.cpu.amd.updateMicrocode = lib.mkDefault config.hardware.enableRedistributableFirmware;
}
