# Nightly fleet auto-upgrade, orchestrated from lampadas.
#
#   02:00  fleet-upgrade.service
#            1. git fetch + npins bump of the nixos-26.05 channel pin in the
#               dedicated checkout (fleet-auto branch, main is untouched)
#            2. deploy (nixos-rebuild switch) every VM, then medano
#               (builds happen on the targets, 4 at a time)
#            3. reboot medano if its kernel changed; unlock its ZFS pools via
#               the initrd sshd, wait for the VMs to come back
#            4. virsh reboot every VM whose toplevel changed
#            5. write results.json + ship it to medano for status-board
#   03:30  fleet-self-upgrade.service
#            lampadas upgrades (switch) and reboots ITSELF when its own
#            kernel changed, after arming the fleet-unlock-lampadas watcher
#            on medano so the LUKS passphrase is fed remotely.
#   08:00  fleet-report.service
#            posts a summary into #emile via syswatch --say: upgrade result,
#            who is back up, stale backups, kernel freshness (kernel.org).
#
# Mutual unlock: lampadas holds medano's ZFS passphrase + an initrd ssh key
# (fleet_unlock_medano_ed25519, authorized in ops/machines/x86/medano/boot.nix);
# medano holds lampadas' LUKS passphrase + key (fleet_unlock_lampadas_ed25519,
# authorized in ops/machines/x86/lampadas/default.nix). Passphrase secrets
# must be filled in by hand - see ops/secrets/secrets.nix.
#
# The checkout owns nothing elsewhere: it is a throwaway clone that the
# service maintains itself.

{ hefe }:
{ config, lib, pkgs, ... }:

let
  cfg = config.fleetUpgrade;

  # the 26.05 pin ships npins 0.4.1, which can neither read (source-format
  # "version 8") nor safely rewrite the repo's sources.json - the pin bump
  # silently no-ops with it (2026-11-03). Use npins from nixos-unstable
  # (0.5.x) like my dry runs did: read+update verified working.
  npinsPkg = (import hefe.third_party."nixos-unstable" {
    system = "x86_64-linux";
    config.allowUnfree = true;
  }).npins;

  ipam = hefe.ops.ipam;

  # All libvirt VMs = actual directories under ops/vms/x86 (plus skipping
  # support dirs/files). Do NOT derive from hefe.ops.nixos attrs: readTree
  # injects junk (__readTree/all/modules/...) that then get deployed as
  # phantom hosts (2026-11-03 morning run).
  #
  # Excluded so fleet eval can't fatally fail:
  #   arr  - imports a gitignored ./private.nix that only exists locally
  #   code - references /Users/emile/.config/opencode (caladan-local path)
  # ssh-only failures (rou) stay in the list: they deploy nothing but are
  # recorded in results.json, so nobody silently pretends they're covered.
  skip = [ "arr" "code" ];
  vms = builtins.attrNames (
    builtins.removeAttrs
      (lib.filterAttrs
        (_: t: t == "directory")
        (builtins.readDir (../. + "/vms/x86")))
      ([ "modules" ] ++ skip)
  );

  # name -> ip from any ipam bridge (default, code, rou, private, tailscale)
  # readTree injects non-attrset metadata values into hefe.* nodes, so only
  # consider attrset-valued children of ipam (the actual bridges).
  bridgesWith = name:
    builtins.filter
      (b: builtins.isAttrs ipam.${b} && builtins.hasAttr name ipam.${b})
      (builtins.attrNames ipam);
  ipOf = name:
    let bridges = bridgesWith name; in
    if bridges == [] then null else ipam.${builtins.head bridges}.${name}.v4;

  sshPortOf = name:
    let bridges = bridgesWith name; in
    if bridges == [] then 22 else (ipam.${builtins.head bridges}.${name}.ports.ssh or 22);

  vmSshConfig = lib.concatMapStringsSep "\n" (name:
    let ip = ipOf name; in
    if ip == null then "" else ''
      Host ${name}
          HostName ${ip}
          User root
          Port ${toString (sshPortOf name)}
          ProxyJump medano-root
          # first-contact failures abort BatchMode nix copies otherwise
          StrictHostKeyChecking accept-new
    '') vms;

  syswatch = pkgs.callPackage hefe.ops.pkgs.syswatch { };

  upgradeScript = pkgs.writeShellApplication {
    name = "fleet-upgrade";
    runtimeInputs = [ pkgs.git npinsPkg pkgs.nix pkgs.jq pkgs.openssh pkgs.netcat-gnu pkgs.coreutils pkgs.gnugrep pkgs.gnused ];
    text = builtins.readFile ./fleet-upgrade.sh;
  };

  reportScript = pkgs.writeShellApplication {
    name = "fleet-report";
    runtimeInputs = with pkgs; [ jq curl openssh coreutils gnugrep ];
    text = builtins.readFile ./fleet-report.sh;
  };

  selfUpgradeScript = pkgs.writeShellApplication {
    name = "fleet-self-upgrade";
    runtimeInputs = with pkgs; [ nix openssh coreutils gnugrep ];
    text = builtins.readFile ./fleet-self-upgrade.sh;
  };
in
{
  options.fleetUpgrade = {
    enable = lib.mkEnableOption ''
      nightly fleet auto-upgrade: npins bump, deploy everything, reboot with
      mutual LUKS/ZFS remote unlock between lampadas and medano'';

    checkoutDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/fleet/hefe3";
      description = "Dedicated throwaway hefe3 checkout (fleet-auto branch).";
    };

    repoUrl = lib.mkOption {
      type = lib.types.str;
      # https: the repo is public, anonymous clone/fetch always works;
      # root@lampadas has no github key and needs none.
      default = "https://github.com/HanEmile/hefe3.git";
      description = "Where to clone when checkoutDir is empty.";
    };

    medanoSsh = lib.mkOption {
      type = lib.types.str;
      default = "95.217.35.60";
      description = "medano's public IP (initrd sshd + unlock target).";
    };

  };

  config = lib.mkIf cfg.enable {
    # ssh wiring for root@lampadas: reach medano (initrd-unlock alias too)
    # and every VM through the medano ProxyJump.
    # NOTE: named medano-root on purpose - /root/.ssh/config carries a stale
    # "Host medano -> User lampadas" entry that beats system config, and the
    # fleet flows need ROOT on medano (2026-11-03).
    programs.ssh.extraConfig = ''
      Host medano-root
          HostName ${cfg.medanoSsh}
          User root
          Port 2222
          IdentityFile /root/.ssh/id_ed25519
          StrictHostKeyChecking accept-new

      Host medano-initrd
          HostName ${cfg.medanoSsh}
          User root
          Port 2222
          IdentityFile ${config.age.secrets.fleet_unlock_medano_ed25519.path}
          # Same IP:port serves either main sshd or the initrd sshd; their
          # host keys differ, and a stale entry for the main key kills the
          # unlock attempt (2026-11-04). Keep a dedicated known_hosts just
          # for the initrd.
          StrictHostKeyChecking accept-new
          UserKnownHostsFile /var/lib/fleet/medano-initrd_known_hosts

      # fleet-self-upgrade deploys "lampadas" to itself; give root a loopback
      # alias instead of relying on decades-old stale user config
      Host lampadas
          HostName 127.0.0.1
          User root
          Port 22
          IdentityFile /root/.ssh/id_ed25519
    '' + vmSshConfig;

    age.secrets = {
      # generated in ops/secrets, private key lives ONLY on lampadas
      fleet_unlock_medano_ed25519 = {
        file = hefe.ops.secrets."fleet_unlock_medano_ed25519.age";
        mode = "0600";
      };
      # USER-FILLED: ZFS passphrase for medano's pools (grave + rpool)
      fleet_medano_zfs_passphrase = {
        file = hefe.ops.secrets."medano_zfs_passphrase.age";
        mode = "0400";
      };
    };

    systemd.services.fleet-upgrade = {
      description = "nightly fleet upgrade: bump nixpkgs pin, deploy VMs + medano, reboot/unlock";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      serviceConfig = {
        Type = "oneshot";
        StateDirectory = "fleet";
        # whole fleet at 4-way concurrency can take a while
        TimeoutStartSec = "3h";
        Nice = 10;
      };
      environment = {
        # systemd leaves HOME empty; git then never sees the safe.directory
        # exception in /root/.gitconfig and refuses the checkout otherwise
        HOME = "/root";
        FLEET_CHECKOUT = cfg.checkoutDir;
        FLEET_REPO_URL = cfg.repoUrl;
        FLEET_MEDANO = cfg.medanoSsh;
        FLEET_VMS = lib.concatStringsSep " " vms;
        FLEET_SYSWATCH = "${syswatch}/bin/syswatch";
        FLEET_ZFS_PASS_FILE = config.age.secrets.fleet_medano_zfs_passphrase.path;
      };
      script = "exec ${upgradeScript}/bin/fleet-upgrade";
    };

    systemd.timers.fleet-upgrade = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = "*-*-* 02:00:00";
        Persistent = true;
      };
    };

    systemd.services.fleet-self-upgrade = {
      description = "lampadas upgrades + reboots itself (medano unlocks it)";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      serviceConfig = {
        Type = "oneshot";
        StateDirectory = "fleet";
        TimeoutStartSec = "1h";
      };
      environment = {
        FLEET_CHECKOUT = cfg.checkoutDir;
        FLEET_MEDANO = cfg.medanoSsh;
      };
      script = "exec ${selfUpgradeScript}/bin/fleet-self-upgrade";
    };

    systemd.timers.fleet-self-upgrade = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = "*-*-* 03:30:00";
        Persistent = true;
      };
    };

    systemd.services.fleet-report = {
      description = "08:00 fleet report into #emile via syswatch --say";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      serviceConfig = {
        Type = "oneshot";
        StateDirectory = "fleet";
        TimeoutStartSec = "5min";
      };
      environment = {
        FLEET_MEDANO = cfg.medanoSsh;
        FLEET_SYSWATCH = "${syswatch}/bin/syswatch";
      };
      script = "exec ${reportScript}/bin/fleet-report";
    };

    systemd.timers.fleet-report = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = "*-*-* 08:00:00";
        Persistent = true;
      };
    };
  };
}
