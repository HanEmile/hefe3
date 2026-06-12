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
    # Pin 6.12 LTS. `latestCompatibleLinuxPackages` is deprecated and on
    # 26.05 resolves to 6.18 (non-LTS) - rebooting this remote-only box
    # into an untested non-LTS kernel is risky, and 6.18 may outrun ZFS
    # support. medano is ALREADY running 6.12.82, so pinning 6.12 makes
    # the next reboot boot the same proven kernel family it runs today.
    kernelPackages = pkgs.linuxPackages_6_12;
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
      # Use the scripted (pre-systemd) initrd. medano relies on scripted-
      # initrd-only features: postDeviceCommands (the SYSINIT rollback
      # below) and network.postCommands (the remote-unlock zfs load-key
      # flow). systemd stage-1 supports neither. Newer nixpkgs default
      # stage-1 to systemd, which now hard-errors on postDeviceCommands;
      # this pins the scripted initrd that medano was always built around.
      systemd.enable = false;

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
