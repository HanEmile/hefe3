# readTree options
{
  hefe,
  lib,
  pkgs,
  ...
}@args1:

# passed by module system
{
  config,
  nixvirt,
  ...
}@args2:

let
  mod = name: hefe.path.origSrc + ("/ops/modules/" + name);
  vm = name: hefe.path.origSrc + ("/ops/vms/x86/" + name + "/libvirt.nix");
in
{
  # QEMU user-mode emulation for armv6l — lets medano build armv6l-linux
  # derivations (RPi 1 BMC images) via transparent emulation.
  boot.binfmt.emulatedSystems = [ "armv6l-linux" ];

  imports = [
    (import ./boot.nix (args1 // args2))
    ./networking.nix
    ./hardware-configuration.nix
    ./libvirt

    hefe.tools.sshrouter.module

    (mod "ports.nix")
    ../../../vms/x86/modules/healthProbes.nix
    (hefe.tools.status-board.module { inherit hefe; })

    (vm "naraj") # general nginx router
    (vm "rou") # route VPN traffic
    (vm "arr") # media
    (vm "auth") # sso
    (vm "md") # hedgedoc
    (vm "git") # git
    (vm "data") # data
    (vm "miki") # md wiki
    (vm "photo") # public immich
    (vm "rss") # rss feed
    (vm "social") # gotosocial
    (vm "tmp") # tmpfile host
    (vm "amalthea") # astrophotography
    (vm "late") # community
    (vm "demo01") # image-bootstrap demo VM
    (vm "sb1") # standby linux vm
    (vm "sb2") # standby linux vm
    (vm "sb3") # standby linux vm
    (vm "minecraft") # minecraft world (NFS /grave/games/minecraft)
    (vm "factorio") # factorio (NFS /grave/games/factorio)
    (vm "r2wars") # radare2 workspace
    (vm "irc") # irc

    # ctf

  ];

  age.secrets = {
    storagebox_bx11_restic_password = {
      file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
    };
    storagebox_bx11_connection_config = {
      file = hefe.ops.secrets."storagebox_bx11_connection_config.age";
    };
  };

  fileSystems = {
    "/proc" = {
      device = "/proc";
      fsType = "proc"; # required since 26.05; was tolerated by older eval
      options = [
        "nosuid"
        "nodev"
        "noexec"
        "relatime" # normal foo

        # mount -o remount,hidepid=2 /proc
        "hidepid=2" # this makes sure users can only see their own processes
      ];
    };

    "/mnt/storagebox-bx11" = {
      device = "//u331921.your-storagebox.de/backup";
      fsType = "cifs";
      options =
        let
          conn_config = config.age.secrets."storagebox_bx11_connection_config".path;
        in
        [
          "_netdev,x-systemd.automount,noauto,x-systemd.idle-timeout=60s,x-systemd.device-timeout=5s,x-systemd.mount-timeout=5s,credentials=${conn_config}"
        ];
    };
  };

  security.acme = {
    acceptTerms = true;
    defaults.email = "letsencrypt@emile.space";
  };

  users =
    let
      aclconf = with hefe.ops.acl; (usersForHost host."${config.networking.hostName}");
    in
    {
      users = aclconf.users // {
        # Just manually adding this here as a sort of "safeguard", I don't want
        # to accidentally remove the key from the ACL and be stuck without a con
        root.openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;
      };
      groups = aclconf.groups;
    };

  environment.systemPackages = with pkgs; [
    vim
    libgbm
    cifs-utils

    # for the networkd-dispatcher service used by tailscale
    ethtool
    networkd-dispatcher
  ];

  hardware.enableRedistributableFirmware = true;

  nix.settings.system-features = [ "nixos-test" "benchmark" "big-parallel" "kvm" "gccarch-armv6kz" "gccarch-x86-64-v2" ];
  nix.settings.trusted-users = [
    "root"
    "@wheel"
  ];

  programs = {
    mosh.enable = true;
  };

  services = {
    openssh = {
      enable = true;
      ports = [
        22
        2222
      ];
      settings.PasswordAuthentication = false;
    };

    tailscale = {
      enable = true;
      extraUpFlags = [ "--ssh --advertise-exit-node" ];
      # interfaceName = "enp0s31f6";
    };

    networkd-dispatcher = {
      enable = true;
      rules."50-tailscale" = {
        onState = [ "routable" ];
        script = ''
          "${pkgs.ethtool}/sbin/ethtool" -K "${config.services.tailscale.interfaceName}" rx-udp-gro-forwarding on rx-gro-list off
        '';
      };
    };

    vnstat.enable = true;

    nfs.server = {
      enable = true;
      exports = ''
        /grave/data            192.168.75.7/32(rw,async,no_root_squash,no_subtree_check)
        /grave/media           192.168.33.3/32(rw,async,no_root_squash,no_subtree_check)
        /grave/games/minecraft 192.168.75.20/32(rw,async,no_root_squash,no_subtree_check)
        /grave/games/factorio  192.168.75.21/32(rw,async,no_root_squash,no_subtree_check)
      '';
      lockdPort = 4001;
      mountdPort = 4002;
      statdPort = 4000;
      extraNfsdConfig = "";
    };

    # SSH Router - routes users to target hosts
    sshrouter = {
      enable = false;
      listenHost = "0.0.0.0";
      listenPort = 22;
      routes = {
        # Add user -> target mappings here
        hanemile = "${hefe.ops.ipam.default.miki.v4}:22";
        root-arr = "${hefe.ops.ipam.private.arr.v4}:22";
      };
      default = "${hefe.ops.ipam.default.naraj.v4}:22";
    };

    # TODO: figure out what zfs datasets to backup
    # sanoid = {
    #   enable = true;
    # };

    nginx.enable = false; # ingress moved to naraj VM
  };

  system.stateVersion = "25.05";

  # External health probes - TLS terminates on naraj now.
  # e1000e (eno1) Hardware Unit Hang mitigation. The 2026-05-29 outage
  # was caused by 12k+ "Detected Hardware Unit Hang" events on eno1
  # starting at 01:35 UTC with TSO/GSO/GRO all enabled. Known workaround
  # for this driver/NIC family is to disable segmentation/large-receive
  # offloads. Run as a oneshot at boot so the state is applied even when
  # the link is already routable - networkd-dispatcher's routable.d hook
  # only fires on state transitions, which won't re-fire on plain switch.
  systemd.services."e1000e-eno1-offloads" = {
    description = "Disable e1000e TX offloads on eno1 (HW unit hang workaround)";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-pre.target" "sys-subsystem-net-devices-eno1.device" ];
    bindsTo = [ "sys-subsystem-net-devices-eno1.device" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      ExecStart = "${pkgs.ethtool}/sbin/ethtool -K eno1 tso off gso off gro off tx off rx off sg off";
    };
  };

  services.healthProbes.probes = [
    { name = "naraj-root";       url = "http://${hefe.ops.ipam.default.naraj.v4}/"; }
    { name = "emile-space";      url = "https://emile.space/"; }
    { name = "auth";             url = "https://sso.emile.space/api/health"; }
    { name = "md";               url = "https://md.emile.space/status"; }
    { name = "photo";            url = "https://photo.emile.space/api/server/ping"; }
    { name = "amalthea";         url = "https://amaltheea.medano.emile.space/"; expectedStatus = 502; }
    { name = "tmp";              url = "https://tmp.emile.space/"; }
    { name = "social";           url = "https://social.emile.space/api/v1/instance"; }
    { name = "status";           url = "https://status.emile.space/"; expectedStatus = 302; }
  ];

  # Prometheus node-exporter for medano itself. Bind on tailscale + localhost
  # only so the host's :9100 is not publicly reachable.
  services.prometheus.exporters.node = {
    enable = true;
    listenAddress = "0.0.0.0";
    port = 9100;
    enabledCollectors = [ "systemd" "logind" "processes" ];
  };

  networking.firewall.interfaces."tailscale0".allowedTCPPorts = [ 9100 ];
}
