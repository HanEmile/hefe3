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
    kernelPackages = config.boot.zfs.package.latestCompatibleLinuxPackages;
    zfs.devNodes = "/dev/disk/by-partlabel";

    kernelParams = [
      "ip=95.217.35.60::95.217.35.1:255.255.255.192:medano:enp0s31f6:off:8.8.8.8:8.8.4.4:"
    ];
    kernelModules = [
      "nvme"
      "xhci_pci"
      "ahci"
      "usb_storage"
      "sd_mod"
      "e1000e"
    ];

    initrd = {
      postDeviceCommands = lib.mkAfter ''
        zfs rollback -r rpool/nixos@SYSINIT
      '';

      kernelModules = [
        "e1000e"
        "aesni_intel"
        "igb"
      ];
      availableKernelModules = [
        "e1000e"
        "cryptd"
        "aesni_intel"
        "igb"
      ];

      network = {
        enable = true;
        ssh = {
          enable = true;
          # To prevent ssh clients from freaking out because a different host key is used,
          # a different port for ssh is useful (assuming the same host has also a regular sshd running)
          port = 2222;
          # hostKeys paths must be unquoted strings, otherwise you'll run into issues with boot.initrd.secrets
          # the keys are copied to initrd from the path specified; multiple keys can be set
          # you can generate any number of host keys using
          # Generating public/private ed25519 key pair.
          hostKeys = [ /medano_host_ed25519_key ];
          # public ssh key used for login
          authorizedKeys = [
            "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPZi43zHEsoWaQomLGaftPE5k0RqVrZyiTtGqZlpWsew emile@caladan"
          ] ++ hefe.users.hanemile.keys.all;

          # postCommands = ''
          #   zpool import -a
          #   echo "zfs load-key -a; killall zfs" >> /root/.profile
          # '';
        };
      };
    };

    loader = {
      efi = {
        efiSysMountPoint = "/boot/efi";
        canTouchEfiVariables = false;
      };

      generationsDir.copyKernels = true;

      grub = {
        enable = true;

        devices = [
          "/dev/nvme0n1"
          "/dev/nvme1n1"
        ];

        efiSupport = true;
        efiInstallAsRemovable = true;

        copyKernels = true;
        zfsSupport = true;

        # Saw this, but haven't tried it out yet, as I'd need a Hetzner KVM to debug
        # it and don't have the time
        # https://github.com/NixOS/nixpkgs/issues/214871#issuecomment-1419075335
        #
        # Currently getting
        #
        # ```
        # updating GRUB 2 menu...
        # mount: /boot/efis/efiboot0: move_mount() failed: No space left on device.
        #        dmesg(1) may have more information after failed mount system call.
        # mount: /boot/efi: move_mount() failed: No space left on device.
        #        dmesg(1) may have more information after failed mount system call.
        # ```
        #
        # mirroredBoots = [
        #   { devices = [ "nodev" ]; path = "/boot"; efiSysMountPoint = "/uefi0"; }
        #   { devices = [ "nodev" ]; path = "/boot"; efiSysMountPoint = "/uefi1"; }
        # ];

        # extraPrepareConfig = ''
        #   mkdir -p /boot/efis
        #   for i in  /boot/efis/*; do mount $i ; done

        #   mkdir -p /boot/efi
        #   mount /boot/efi
        # '';

        extraInstallCommands = ''
          ESP_MIRROR=$(${pkgs.coreutils}/bin/mktemp -d)
          ${pkgs.coreutils}/bin/cp -r /boot/efi/EFI $ESP_MIRROR
          for i in /boot/efis/*; do
            ${pkgs.coreutils}/bin/cp -r $ESP_MIRROR/EFI $i
          done
          ${pkgs.coreutils}/bin/rm -rf $ESP_MIRROR
        '';

      };
    };
  };
}
