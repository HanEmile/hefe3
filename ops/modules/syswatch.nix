# syswatch everywhere: failed units, stale restic backups
# (no successful run in >26h), disk/mem/load threshold alerts into #emile.
#
# Lampadas ran this inline since 2026-10-28. The 2026-11-15 self-recovery
# review found the coverage fleet-blind: VMs, mail and medano had no
# unit-failure or backup-freshness bot at all - a stuck restic lock or a
# crashed unit would rot silently until someone happened to look.
#
# Import pattern (like chirpstack.nix):
#   (import ../../../modules/syswatch.nix { inherit hefe pkgs; })
{ hefe, pkgs }:
{ config, lib, ... }:

let
  cfg = config.syswatchBot;
  syswatch = pkgs.callPackage hefe.ops.pkgs.syswatch { };
in
{
  options.syswatchBot = {
    enable = lib.mkEnableOption "syswatch anomaly bot (failed units, stale backups, disk/mem/load) posting to #emile";
    server = lib.mkOption {
      type = lib.types.str;
      default = "irc.emile.space:6697";
      description = "IRC server host:port to post to (public ingress path; the medano-down case is covered by lampadas' fleet-sentry, not this channel).";
    };
    tls = lib.mkOption {
      type = lib.types.bool;
      default = true;
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.syswatch = {
      description = "Post system anomaly alerts to #emile";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      wantedBy = [ "multi-user.target" ];
      # systemctl on PATH for the failed-unit / backup-unit queries.
      path = [ config.systemd.package ];
      serviceConfig = {
        DynamicUser = true;
        StateDirectory = "syswatch";
        ExecStart = "${syswatch}/bin/syswatch ${lib.optionalString cfg.tls "--tls"} --server ${cfg.server} --channel '#emile'";
        # AF_UNIX for the D-Bus systemctls, AF_INET/AF_INET6 for IRC + DNS.
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        Restart = "always";
        RestartSec = "10s";
      };
    };
  };
}
