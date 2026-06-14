# Parella — headless bare-metal box.
#
# B450 Gaming-ITX/ac, Ryzen 5 2600, 8GB DDR4-2133, 250GB WD Blue NVMe,
# Intel Xeon Phi 3120P (PCIe). No discrete GPU — runs headless with
# serial console + SSH. Encrypted ZFS (rpool, native encryption) with
# systemd-boot/UEFI and initrd-ssh remote unlock.
#
# Build the installer ISO:
#   nix-build -A ops.nixos.parella.iso
#   dd if=./result/iso/*.iso of=/dev/sdX bs=4M status=progress
#
# The ISO auto-installs: boot it, SSH in, run `parella-install`, done.
# After install, subsequent deploys from caladan:
#   nix-build -A ops.nixos.parella.deploy && ./result/bin/deploy
{
  hefe,
  pkgs,
  lib,
  ...
}@args1:

{
  config,
  ...
}@args2:

{
  imports = [
    (import ./boot.nix (args1 // args2))
    ./hardware-configuration.nix
  ];

  networking = {
    hostName = "parella";
    # ZFS requires a stable hostId. Generate at install time with
    # `head -c4 /dev/urandom | od -A none -t x4`. Replace this
    # placeholder after the first install.
    hostId = "ebac5691";

    domain = "home.arpa.";
    search = [ "home.arpa" ];

    useDHCP = lib.mkDefault true;

    nameservers = [
      "8.8.8.8"
      "8.8.4.4"
      "1.1.1.1"
    ];

    firewall = {
      enable = true;
      allowedTCPPorts = [ ];
    };
  };

  time.timeZone = "Europe/Berlin";

  hardware.enableRedistributableFirmware = true;

  zramSwap = {
    enable = true;
    memoryPercent = 50;
  };

  users =
    let
      aclconf = with hefe.ops.acl; (usersForHost host."${config.networking.hostName}");
    in
    {
      mutableUsers = false;
      users = aclconf.users // {
        root = aclconf.users.root or { } // {
          openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;
        };
        emile = aclconf.users.emile // {
          extraGroups = (aclconf.users.emile.extraGroups or [ ]) ++ [
            "wheel"
          ];
        };
      };
      groups = aclconf.groups;
    };

  environment.systemPackages = with pkgs; [
    vim
    git
    tmux
    htop
    dust
    tailscale
    ethtool
    dmidecode
    pciutils
    usbutils
    lm_sensors
  ];

  programs.mosh.enable = true;

  services = {
    openssh = {
      enable = true;
      settings = {
        PasswordAuthentication = false;
        KbdInteractiveAuthentication = false;
      };
    };

    tailscale = {
      enable = true;
      extraUpFlags = [ "--ssh" ];
    };

    vnstat.enable = true;

    zfs.autoScrub.enable = true;

    prometheus.exporters.node = {
      enable = true;
      listenAddress = "0.0.0.0";
      port = 9100;
      enabledCollectors = [
        "systemd"
        "logind"
        "processes"
      ];
    };
  };

  networking.firewall.interfaces."tailscale0".allowedTCPPorts = [ 9100 ];

  nix = {
    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 14d";
    };
    settings = {
      auto-optimise-store = true;
    };
  };

  system.stateVersion = "25.05";
}
