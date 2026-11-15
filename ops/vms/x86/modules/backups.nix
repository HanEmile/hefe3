# Standard per-VM restic backup module.
#
# Usage in a VM's default.nix:
#
#   imports = [
#     ./hardware-image.nix
#     (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
#     (import ../modules/backups.nix { inherit hefe; })
#   ];
#
#   vmBackups = {
#     paths = [ "/var/lib/hedgedoc" ];
#     # optional:
#     #   excludePatterns = [ "*.tmp" "/var/lib/hedgedoc/uploads/.cache" ];
#     #   onCalendar = "daily";   # default: daily
#     #   pruneOpts = [ ... ];    # default: keep-daily 7 weekly 5 monthly 12 yearly 15
#     #   backupPrepareCommand = "pg_dump -U postgres mydb > /var/lib/mydb.dump";
#   };
#
# Repository: /mnt/storagebox-bx11/backup/<hostname>
# Storagebox is mounted via CIFS (lazy automount). Password and connection
# config secrets come from agenix and must be keyed for this host in
# ops/secrets/secrets.nix.

{ hefe }:

{ config, lib, pkgs, ... }:

let
  cfg = config.vmBackups;
  hostname = config.networking.hostName;
in
{
  # Provides the CIFS mount + connection-config secret shared with
  # medano/mail/lampadas.
  imports = [
    (import ../../../modules/storagebox.nix { inherit hefe; })
  ];

  options.vmBackups = {
    enable = lib.mkOption {
      type = lib.types.bool;
      default = (cfg.paths or []) != [];
      defaultText = lib.literalExpression "paths != []";
      description = "Enable restic backups for this VM. Defaults to true when paths is non-empty.";
    };

    paths = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "Paths to back up. The VM's name is used as the repository sub-dir.";
      example = [ "/var/lib/hedgedoc" "/var/lib/postgresql" ];
    };

    excludePatterns = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "restic --exclude patterns.";
    };

    onCalendar = lib.mkOption {
      type = lib.types.str;
      default = "*-*-* 03:17:00";
      description = "systemd OnCalendar spec. Default: 03:17 daily; spread by RandomizedDelaySec=1h in the timer so all VMs do not hammer storagebox at once.";
    };

    pruneOpts = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
        "--keep-daily 7"
        "--keep-weekly 5"
        "--keep-monthly 12"
        "--keep-yearly 15"
      ];
      description = "restic forget --prune options.";
    };

    backupPrepareCommand = lib.mkOption {
      type = lib.types.nullOr lib.types.lines;
      default = null;
      description = "Optional shell snippet to run before the backup (e.g. pg_dump).";
    };
  };

  config = lib.mkIf cfg.enable {
    age.secrets = {
      storagebox_bx11_restic_password = {
        file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
      };
    };

    # The storagebox CIFS automount is set up in the host namespace and
    # does not propagate into restic's private mount namespace (PrivateTmp
    # etc.) -> ENODEV and the backup fails permanently for the night.
    # RequiresMountsFor pulls it in; this was hand-patched onto lampadas,
    # medano and mail per-host since 2026-06 while the shared VM module
    # kept generating broken units (2026-11-15 review).
    systemd.services."restic-backups-${hostname}".unitConfig.RequiresMountsFor = "/mnt/storagebox-bx11";

    services.restic.backups."${hostname}" = {
      timerConfig = {
        OnCalendar = cfg.onCalendar;
        Persistent = true;
        RandomizedDelaySec = "1h";
      };
      repository = "/mnt/storagebox-bx11/backup/${hostname}";
      initialize = true;
      passwordFile = config.age.secrets."storagebox_bx11_restic_password".path;
      paths = cfg.paths;
      exclude = cfg.excludePatterns;
      pruneOpts = cfg.pruneOpts;
      backupPrepareCommand = cfg.backupPrepareCommand;
    };
  };
}
