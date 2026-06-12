# lernaeus - GPU workstation/box.
#
# Single 512GB NVMe SSD, 32GB RAM, NVIDIA RTX A2000 6GB. Encrypted ZFS
# (rpool, native encryption) with systemd-boot + UEFI and initrd-ssh
# remote unlock. See ./README.md for the install runbook.
#
# readTree passes the first arg-set (hefe/lib/pkgs); the NixOS module
# system passes the second (config/...). boot.nix and gpu.nix take both.
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
    (import ./gpu.nix (args1 // args2))
    (import ./sway.nix (args1 // args2))
    (import ./gaming.nix (args1 // args2))
    (import ./fans.nix (args1 // args2))
    ./hardware-configuration.nix
  ];

  networking = {
    hostName = "lernaeus";
    # hostId is required by ZFS; must be unique and stable. Generated at
    # install time with `head -c4 /dev/urandom | od -A none -t x4`. MUST
    # match the value baked into the on-disk install, or the pool won't
    # auto-import.
    hostId = "51392d58";

    domain = "home.arpa."; # RFC 8375, matches lampadas
    search = [ "home.arpa" ];

    useDHCP = lib.mkDefault true;

    nameservers = [
      "8.8.8.8"
      "8.8.4.4"
      "1.1.1.1"
    ];

    firewall = {
      enable = true;
      # node-exporter is opened on the tailscale interface only (below).
      allowedTCPPorts = [ ];
    };
  };

  time.timeZone = "Europe/Berlin";

  hardware.enableRedistributableFirmware = true;

  # Compressed RAM swap instead of a disk swap device. 32GB RAM box; this
  # avoids the rpool/swap zvol whose /dev/zvol udev symlink raced the
  # scripted-initrd device wait and wedged boot. zram never blocks boot.
  zramSwap = {
    enable = true;
    memoryPercent = 50;
  };

  # Users are derived from the per-host ACL (ops/acl/default.nix), exactly
  # like medano. root's key is pinned here as a safeguard.
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
        # emile is the autologin/sway/gaming user (created by the ACL's
        # withDefault, which sets isNormalUser + authorizedKeys). Merge ON
        # TOP of the ACL entry (don't replace it) to add the desktop+gaming
        # groups: input/video for sway, audio for sound, gamemode for the
        # gamemoded perf governor, wheel for sudo.
        emile = aclconf.users.emile // {
          extraGroups = (aclconf.users.emile.extraGroups or [ ]) ++ [
            "wheel"
            "video"
            "input"
            "audio"
            "gamemode"
          ];
          # Local-console password (yescrypt hash of a known passphrase).
          # Committing the hash here is acceptable for THIS box because:
          #  - SSH password auth is disabled (services.openssh.settings
          #    .PasswordAuthentication = false + KbdInteractiveAuthentication
          #    = false), so this hash can ONLY be used at the physical
          #    console / lock screen, never remotely.
          #  - lernaeus is a single-user LAN gaming box behind full-disk
          #    encryption; the disk passphrase is the real security gate.
          #  - mutableUsers = false would otherwise wipe any `passwd`-set
          #    password on every deploy; declaring it keeps console login
          #    working across rebuilds.
          # Rotate by replacing this hash: `mkpasswd -m yescrypt`.
          hashedPassword = "$y$j9T$wpNdEESbWz8.Q7ZcfySvW0$X/iaWpKcGJWiGSSP2pku8TaA/a.5hsWkxe2Ww3XCt18";
        };
      };
      groups = aclconf.groups;
    };

  nix.settings = {
    trusted-users = [
      "root"
      "@wheel"
    ];

    # Pull cached store paths from medano (over ssh-ng) in addition to the
    # public cache. medano has most of the fleet's closures already built,
    # so building lernaeus locally substitutes from medano first, then
    # cache.nixos.org, and only compiles what neither has (e.g. the nvidia
    # kernel module). lernaeus's nix-build@lernaeus key is authorized on
    # medano via ops/acl (medano.lernaeus). require-sigs is relaxed for the
    # ssh substituter since medano's store paths aren't signed.
    substituters = [
      "ssh-ng://lernaeus@medano.emile.space"
      "https://cache.nixos.org/"
    ];
    trusted-substituters = [
      "ssh-ng://lernaeus@medano.emile.space"
    ];
    require-sigs = false;
  };

  # The nix daemon (root) needs to know medano's host key and which
  # identity to use when connecting as the substituter. The build-pull key
  # was generated at /root/.ssh/id_ed25519 during bring-up.
  programs.ssh.knownHosts."medano.emile.space".publicKey =
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDo2MqY7BD6rd/L3UURx/2kTuHMC7V7WmW74bCsejChq";

  environment.systemPackages = with pkgs; [
    vim
    git
    tmux
    htop
    dust
    tailscale
    ethtool
    dmidecode
  ];

  programs.mosh.enable = true;

  services = {
    openssh = {
      enable = true;
      ports = [
        22
        2222
      ];
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

    # Weekly scrub for the single-disk pool (no redundancy, but scrub
    # still surfaces checksum errors before they bite).
    zfs.autoScrub.enable = true;

    # Prometheus node-exporter, tailscale-only (mirrors medano).
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
    settings.auto-optimise-store = true;
  };

  system.stateVersion = "25.05";
}
