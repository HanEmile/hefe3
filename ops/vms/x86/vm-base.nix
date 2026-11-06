# usage:
# imports = [
#   (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
# ]

# the host to run on (this is used to set the gateway ip), and the default bridge to use.
{ vmhost, primaryBridge ? "default" }:

# the hefe module layer
{ hefe, pkgs, ... }:

# the "normal" module layer passed by imports
{ config, ... }:

let
  acl = hefe.ops.acl;
  hostname = config.networking.hostName;
in
{
  imports = [
    ../../modules/generation-canary.nix
  ];

  boot.loader.grub.enable = true;
  boot.loader.grub.device = "/dev/sda";

  time.timeZone = "Europe/Helsinki";

  documentation.nixos.enable = false;

  users = let
    aclconf = with acl; (usersForHost host."${hostname}");
  in {
    groups = aclconf.groups;
    # fleet-upgrade deploys VMs from lampadas - root@lampadas' key reaches
    # VMs through this base module; careful merging, not // at top level.
    users = aclconf.users // {
      root = (aclconf.users.root or {}) // {
        openssh.authorizedKeys.keys =
          # KEEP existing fleet/acl root keys! The first version of this
          # concat clobbered them (2026-11-03 lockout, restored via lampadas).
          (
            # 'attr.path or D ++ X' parses as 'attr.path or (D ++ X)' meaning
            # keys stayed acl-only when the attr existed - the missing parens
            # are why lampadas' key was absent on newly-deployed VMs
            # (2026-11-06).
            aclconf.users.root.openssh.authorizedKeys.keys or []
          ) ++ [
            "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIARD1yc+6LpvGg5JcFc2jDFBv3kKRLXeNYqqFnCmAHkZ root@lampadas"
          ];
      };
    };
  };

  environment.systemPackages = with pkgs; [
    vim
    htop
    git
    tailscale
    vnstat
    ripgrep
  ];

  networking = {
    useDHCP = false;

    interfaces = {
      "enp1s0" = {
        useDHCP = false;
        macAddress = hefe.ops.ipam."${primaryBridge}"."${hostname}".mac;
        ipv4.addresses = [
          {
            address = hefe.ops.ipam."${primaryBridge}"."${hostname}".v4;
            prefixLength = 24;
          }
        ];
      };
    };

    defaultGateway = {
      address = hefe.ops.ipam."${primaryBridge}"."${vmhost}".v4;
      interface = "enp1s0";
    };

    nameservers = [ "8.8.8.8" ];

    firewall = {
      enable = true;
      # node-exporter on :9100 reachable from both tailscale and the bridge IP
      # so the medano-hosted status-board can scrape every VM uniformly.
      interfaces."tailscale0".allowedTCPPorts = [ 80 443 9100 ];
      interfaces."enp1s0".allowedTCPPorts = [ 9100 ];
      allowedTCPPorts = [ ];
      allowedUDPPorts = [ ];
    };
  };

  services = {
    openssh = {
      enable = true;
      authorizedKeysInHomedir = true; # enables ~/.ssh/authorized_keys
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

    # Prometheus node-exporter - base metrics for every VM, scraped from
    # medano over tailscale.
    prometheus.exporters.node = {
      enable = true;
      listenAddress = "0.0.0.0";
      port = 9100;
      enabledCollectors = [ "systemd" "logind" "processes" ];
    };
  };

  nix = {
    gc = {
      automatic = true;
      # Daily: the nightly fleet-upgrade (02:00) pulls a fresh multi-GiB
      # closure per VM; a weekly cadence let a single deploy wave fill the
      # tiny 5-9 GiB roots to 100% (rss/sb/minecraft/... 2026-11-03).
      dates = "daily";
      # The small appliances have 5-6 GiB roots where two generations of a
      # fat /nix/store barely fit. With nightly deploys, retaining a week of
      # generations pins several closures at once - three days still leaves
      # the previous generation around for rollbacks, without hoarding.
      options = "--delete-older-than 3d";
    };
    settings = {
      auto-optimise-store = true;
    };
  };

  # Same story: uncapped journals were ~500 MiB per VM on 5 GiB roots.
  services.journald.extraConfig = ''
    SystemMaxUse=100M
    MaxFileSec=1week
  '';

  # --- stability: stay ssh-able even when something inside the VM runs wild --
  # Self-healing: kill runaway user-session cgroups under sustained memory
  # pressure (PSI) instead of letting a job (build, scanner, agent) wedge
  # the whole VM - 2026-11-01: social's sshd could no longer send a banner.
  systemd.oomd.enable = true;
  # sshd is never reclaimed and always wins CPU/IO under contention.
  systemd.services.sshd.serviceConfig = {
    MemoryMin = "64M";
    CPUWeight = 1000;
    IOWeight = 1000;
  };

  # Roll back automatically if a new generation breaks ssh/health at boot.
  generationCanary.enable = true;
}
