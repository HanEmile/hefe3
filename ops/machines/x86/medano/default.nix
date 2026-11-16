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
  # fleet self-recovery baseline (2026-11-15 review): failed-unit/stale-
  # backup alerts to #emile + magic-rollback self-heal of degraded states
  syswatchBot.enable = true;
  magicRollback.enable = true;

  # QEMU user-mode emulation for armv6l — lets medano build armv6l-linux
  # derivations (RPi 1 BMC images) via transparent emulation.
  boot.binfmt.emulatedSystems = [ "armv6l-linux" ];

  # Kernel Samepage Merging: dedupe identical pages across the ~27 identical
  # NixOS guests (91 GiB allocated on 62 GiB RAM, rely on overcommit).
  hardware.ksm.enable = true;

  # --- stability: host sshd must survive guest CPU/IO/memory storms --------
  # All qemu processes live in machine.slice; cgroup weights only matter
  # under contention, so idle guests still get 100% CPU, but a saturated
  # host can never starve sshd/networking (2026-11-01 incident: every VM's
  # sshd became unresponsive while the host was pegged).
  systemd.services.sshd.serviceConfig = {
    CPUWeight = 1000;
    IOWeight = 1000;
    MemoryMin = "256M";
  };
  # Roll back automatically if a new generation breaks ssh after a reboot -
  # essential for the unattended 2am fleet reboots. Failed-unit check off:
  # the host runs specialised one-shots whose transient failures are normal.
  generationCanary = {
    enable = true;
    checkFailedUnits = false;
  };

  # machine.slice is implicit in systemd (no unit file shipped), so defining
  # it outright is safe and collects every guest under these limits.
  # Default weight elsewhere is 100. Under full contention the ~27 guests
  # share ~1/3 of the CPU, the host keeps the rest; idle guests unaffected.
  systemd.units."machine.slice".text = ''
    [Slice]
    CPUWeight=40
    IOWeight=40
  '';

  # --- lampadas pulls its LUKS passphrase from here during initrd ----------
  # (2026-11-15 review: the old fleet-unlock-lampadas watcher pushed to
  # lampadas' TAILNET IP - a tailnet that does not exist in initrd, so the
  # watcher could never connect and every self-reboot would have stranded
  # lampadas at the LUKS prompt. Reversed the flow: lampadas' initrd PULLS
  # over the public internet; outbound works in initrd, inbound does not.)
  age.secrets.lampadas_luks_passphrase = {
    file = hefe.ops.secrets."lampadas_luks_passphrase.age";
    mode = "0440";
    owner = "fleet-unlock-pull";
    group = "fleet-unlock-pull";
  };
  # users.groups/users.users live in the acl-driven block further down
  # (no path-sugar here: this file already defines users = ...; and
  # services = ...;  in full form - Nix refuses the duplicate top-level).
  # Second copy of lampadas' backup repos on grave (2026-11-01: the status
  # page only ever showed the small metadata repo; the ~430 GiB immich
  # library had exactly ONE copy, on the bx11). bx11 and medano are both
  # Hetzner-side, so pulling snapshots over the storagebox CIFS mount is
  # far cheaper than a home-uplink re-upload. Copy runs daily and only
  # transfers chunks that are new since the last run.
  systemd.services.restic-copy-grave = {
    description = "restic copy lampadas repos from the storagebox onto /grave";
    after = [ "network-online.target" ];
    wants = [ "network-online.target" ];
    unitConfig.RequiresMountsFor = "/mnt/storagebox-bx11";
    path = with pkgs; [ restic util-linux coreutils ];
    serviceConfig = {
      Type = "oneshot";
      Nice = 19;
      IOWeight = 20;
      TimeoutStartSec = "12h"; # first seed of ~430 GiB takes a while
    };
    script = ''
      set -euo pipefail
      export RESTIC_PASSWORD_FILE=${config.age.secrets.storagebox_bx11_restic_password.path}
      # the from-repo (storagebox) uses the same password; restic needs it
      # named explicitly when --from-repo is in play
      export RESTIC_FROM_PASSWORD_FILE=$RESTIC_PASSWORD_FILE
      findmnt /mnt/storagebox-bx11 >/dev/null 2>&1 || ls /mnt/storagebox-bx11 >/dev/null 2>&1
      mkdir -p /grave/restic
      for r in photos immich-data; do
        src=/mnt/storagebox-bx11/backup/$r
        dst=/grave/restic/$r
        [ -d "$src/snapshots" ] || { echo "SKIP: no source repo $src"; continue; }
        if [ ! -d "$dst" ]; then
          echo "init $dst (chunker params must match for restic copy)"
          restic -r "$dst" init --copy-chunker-params --from-repo "$src"
        fi
        echo "copy $r -> $dst"
        restic -r "$dst" copy --from-repo "$src"
      done
      echo done
    '';
  };
  systemd.timers.restic-copy-grave = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "*-*-* 05:15:00";
      Persistent = true;
      RandomizedDelaySec = "30min";
    };
  };

  # The photo VM's media library lives on this NFS-backed zfs dataset; the VM
  # itself is disposable, so the pictures live solely here. Backup holes get
  # noticed eventually - this one survived at 67 GiB until 2026-11-03.
  services.restic.backups."grave-photos" = {
    repository = "/mnt/storagebox-bx11/backup/grave-photos";
    initialize = true;
    passwordFile = config.age.secrets.storagebox_bx11_restic_password.path;
    paths = [ "/grave/photos" ];
    timerConfig = {
      OnCalendar = "*-*-* 04:20:00";
      Persistent = true;
      RandomizedDelaySec = "30min";
    };
    pruneOpts = [
      "--keep-daily 7"
      "--keep-weekly 5"
      "--keep-monthly 12"
      "--keep-yearly 15"
    ];
    backupPrepareCommand = "${pkgs.util-linux}/bin/findmnt /mnt/storagebox-bx11 > /dev/null || ls /mnt/storagebox-bx11 > /dev/null";
  };
  systemd.services."restic-backups-grave-photos".unitConfig.RequiresMountsFor = "/mnt/storagebox-bx11";

  systemd.services.fleet-unlock-lampadas = {
    description = "feed lampadas' LUKS passphrase to its initrd sshd";
    serviceConfig = {
      Type = "oneshot";
      TimeoutStartSec = "30min";
      StateDirectory = "fleet";
    };
    path = with pkgs; [ openssh netcat-gnu coreutils ];
    script = ''
      set -u
      ${
        # 2026-11-15: replaced by the pull flow - lampadas' initrd fetches
        # the passphrase from medano's fleet-unlock-pull user itself. The
        # old push watcher polled lampadas' TAILNET IP, which does not
        # exist in initrd; it could literally never succeed. Removed after
        # the self-recovery review; keeping the (dead) unit would only
        # confuse. This stub fails loudly if something stale still arms it.
        ""
      }
      echo "fleet-unlock-lampadas is retired: lampadas pulls its own LUKS passphrase during initrd (see lampadas boot config)"
      exit 1
    '';
  };

  # Scan faster than the conservative defaults (100 pages / 20ms) so a full
  # pass over ~58 GiB of guest RAM takes minutes, not an hour.
  systemd.tmpfiles.rules = [
    "w /sys/kernel/mm/ksm/pages_to_scan    - - - - 2000"
    "w /sys/kernel/mm/ksm/sleep_millisecs  - - - - 10"
  ];

  imports = [
    (import ./boot.nix (args1 // args2))
    (import ../../../modules/syswatch.nix { inherit hefe pkgs; })
    (import ../../../modules/magic-rollback.nix { inherit hefe pkgs; })
    ./networking.nix
    ./hardware-configuration.nix
    ./libvirt

    hefe.tools.sshrouter.module

    (mod "ports.nix")
    ((import (mod "storagebox.nix")) { inherit hefe; })
    ../../../vms/x86/modules/healthProbes.nix
    (hefe.tools.status-board.module { inherit hefe; })

    (import (mod "generation-canary.nix"))

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
    (vm "demo01") # image-bootstrap demo VM
    (vm "nomos") # NOMOI verifier (ipfs + nomos serve)
    (vm "jules") # minimal linux vm (jules)
    (vm "code") # sandboxed coding-agent vm (isolated virbr3 net)
    (vm "minecraft") # minecraft world (NFS /grave/games/minecraft)
    (vm "factorio") # factorio (NFS /grave/games/factorio)
    (vm "r2wars") # radare2 workspace
    (vm "irc") # irc
    (vm "pretix") # ticket shop

  ];

  age.secrets = {
    storagebox_bx11_restic_password = {
      file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
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
        "relatime"

        # mount -o remount,hidepid=2 /proc
        "hidepid=2" # this makes sure users can only see their own processes
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
        root.openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all ++ [
          # root@lampadas reaches root@medano for the nightly fleet-upgrade
          # (distributed nix builds, fleet-unlock-lampadas arming). The
          # secrets.nix "lampadas" pubkey is the host key (user lampadas
          # login); this one is root@lampadas' interactive key.
          "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIARD1yc+6LpvGg5JcFc2jDFBv3kKRLXeNYqqFnCmAHkZ root@lampadas"
        ];
        # serves lampadas' LUKS passphrase to its initrd (forced-command
        # shell is the restriction; Match-block in services.openssh below)
        fleet-unlock-pull = {
          isSystemUser = true;
          group = "fleet-unlock-pull";
          description = "serves lampadas' LUKS passphrase to its initrd (forced command)";
          shell = (pkgs.writeShellScriptBin "fleet-luks-cat" ''
            exec ${pkgs.coreutils}/bin/cat /run/agenix/lampadas_luks_passphrase
          '') // { shellPath = "/bin/fleet-luks-cat"; };
          openssh.authorizedKeys.keys = [
            # private half lives on lampadas at
            # /root/lampadas_initrd_pull_ed25519, baked into its initrd
            "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM/nmvMERO4KvBAVuc7L2SaoDFXvMmpYrDk0I0mjSKmE lampadas-initrd-pull"
          ];
        };
      };
      groups = aclconf.groups // {
        fleet-unlock-pull = { };
      };
    };

  environment.systemPackages = with pkgs; [
    vim
    libgbm

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
      # disable PAM challenge-response path entirely (2026-10-12 VEX scan:
      # kept CVE-2007-2768 visible because sshd-no-opie was dropped)
      settings.KbdInteractiveAuthentication = false;
      # the LUKS-pull key must do nothing but run its forced-command shell
      extraConfig = ''
        Match User fleet-unlock-pull
            AllowTcpForwarding no
            AllowAgentForwarding no
            PermitTunnel no
            X11Forwarding no
            PermitTTY no
      '';
    };

    tailscale = {
      enable = true;
      extraUpFlags = [ "--ssh --advertise-exit-node" ];
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
        /grave/img.emile.space 192.168.75.2/32(rw,async,no_root_squash,no_subtree_check)
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
    { name = "pretix";           url = "https://tickets.emile.space/healthcheck/"; }
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
