# Hardware configuration for Raspberry Pi 1 Model B+ v1.2.
# BCM2835 SoC, ARM1176JZF-S (ARMv6), 512 MB RAM, SD card boot.
{ lib, ... }:

{
  boot.initrd.availableKernelModules = [
    "usbhid"
    "usb_storage"
  ];

  fileSystems."/" = {
    device = "/dev/disk/by-label/NIXOS_SD";
    fsType = "ext4";
  };

  fileSystems."/boot/firmware" = {
    device = "/dev/disk/by-label/FIRMWARE";
    fsType = "vfat";
  };

  swapDevices = [ ];
}
