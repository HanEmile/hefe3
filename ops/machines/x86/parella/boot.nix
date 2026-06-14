# Boot configuration for parella.
#
# Single 250GB WD Blue NVMe SSD, UEFI, systemd-boot. The root pool
# `rpool` uses ZFS native encryption (aes-256-gcm, passphrase
# keylocation=prompt) at the pool root.
#
# Remote unlock: systemd initrd with SSH on port 2222. On login,
# systemd-tty-ask-password-agent runs automatically and prompts for
# the ZFS passphrase. After entering it, boot continues.
#
#   ; ssh -p 2222 root@parella    # or parella-unlock alias
#   (enter passphrase, boot continues)
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  boot = {
    supportedFilesystems = [ "zfs" ];
    kernelPackages = pkgs.linuxPackages_6_12;

    zfs.forceImportRoot = false;
    zfs.devNodes = "/dev/disk/by-id";
    zfs.requestEncryptionCredentials = true;

    loader = {
      systemd-boot = {
        enable = true;
        configurationLimit = 20;
      };
      efi.canTouchEfiVariables = true;
    };

    kernelParams = [
      "console=tty0"
      "console=ttyS0,115200n8"
    ];

    initrd = {
      # Systemd initrd: supports systemd-ask-password over SSH for
      # headless ZFS encryption unlock. The scripted initrd blocks on
      # /dev/console which is useless without a GPU.
      systemd.enable = true;

      availableKernelModules = [
        "nvme"
        "xhci_pci"
        "ahci"
        "usbhid"
        "usb_storage"
        "sd_mod"
        # Intel I211 GbE on the B450 Gaming-ITX/ac.
        "igb"
        "e1000e"
        "r8169"
        "alx"
        # AES acceleration for ZFS native encryption.
        "aesni_intel"
        "cryptd"
      ];
      kernelModules = [ "aesni_intel" ];

      # Networking: systemd-networkd DHCP (not kernel ip=dhcp).
      network.enable = true;
      systemd.network = {
        enable = true;
        networks."10-dhcp" = {
          matchConfig.Name = [ "en*" "eth*" ];
          networkConfig.DHCP = "ipv4";
        };
      };

      # SSH in the initrd for remote ZFS unlock.
      network.ssh = {
        enable = true;
        port = 2222;
        hostKeys = [ "/etc/secrets/initrd/ssh_host_ed25519_key" ];
        authorizedKeys = hefe.users.hanemile.keys.all;
      };

      # On SSH login, auto-run the systemd password agent so the user
      # is immediately prompted for the ZFS passphrase.
      systemd.services.zfs-unlock-profile = {
        description = "Write ZFS unlock profile for SSH login";
        wantedBy = [ "initrd.target" ];
        before = [ "initrd-root-fs.target" ];
        unitConfig.DefaultDependencies = false;
        serviceConfig.Type = "oneshot";
        script = ''
          mkdir -p /var/empty
          echo 'systemd-tty-ask-password-agent --watch' > /var/empty/.profile
        '';
      };
    };
  };
}
