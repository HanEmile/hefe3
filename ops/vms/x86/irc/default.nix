{ hefe, pkgs, ... }:
{ config, lib, ... }:

let
  tsHost = "irc.pinto-pike.ts.net";
  certDir = "/var/lib/tailscale-certs";

  # Ergo: public IRC server for the community. naraj terminates TLS for
  # irc.emile.space and stream-proxies plaintext IRC to this listener, so
  # Ergo itself speaks plaintext on the bridge. medano DNATs :6697 -> naraj.
  ergoPlainPort = 6667;

  # soju: personal bouncer, tailscale-only. Aggregates the local Ergo plus
  # any external networks (Libera, OFTC, ...) behind one always-on session.
  # TLS via a tailscale-issued cert so senpai connects with ircs://.
  sojuTlsPort = 6698;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
    ../modules/tailscale-cert-renew.nix
  ];

  networking.hostName = "irc";
  system.stateVersion = "25.05";

  networking.firewall = {
    # Ergo plaintext: only reachable from the bridge (naraj proxies in) and
    # tailscale (you, directly). Never exposed publicly in plaintext.
    interfaces."enp1s0".allowedTCPPorts = [ ergoPlainPort ];
    interfaces."tailscale0".allowedTCPPorts = [ ergoPlainPort sojuTlsPort ];
  };

  # ---------------------------------------------------------------------------
  # Ergo - public IRC server (https://ergo.chat)
  # ---------------------------------------------------------------------------
  services.ergochat = {
    enable = true;
    settings = {
      network.name = "emilespace";
      server = {
        name = "irc.emile.space";
        # Plaintext listener (naraj does TLS). Bound to all interfaces; the
        # firewall above restricts who can reach it.
        listeners.":${toString ergoPlainPort}" = { };
        # naraj is the proxy in front of us: trust its forwarded source IPs
        # so cloaks/bans reflect the real client, not naraj.
        "proxy-allowed-from" = [ "localhost" hefe.ops.ipam.default.naraj.v4 ];
        "ip-cloaking" = {
          enabled = true;
          netname = "emilespace";
          "cidr-len-ipv4" = 32;
          "cidr-len-ipv6" = 64;
          "num-bits" = 64;
        };
      };
      accounts = {
        "authentication-enabled" = true;
        registration = {
          enabled = true;
          "allow-before-connect" = true;
          "email-verification".enabled = false;
        };
        # Built-in bouncer behaviour: one account, multiple clients, always-on
        # with server-side history replay.
        multiclient = {
          enabled = true;
          "allowed-by-default" = true;
          "always-on" = "opt-in";
          "auto-away" = "opt-in";
        };
      };
      channels = {
        "default-modes" = "+ntC";
        registration.enabled = true;
      };
      # Persistent message history in the datastore (survives restarts; the
      # whole /var/lib/ergo dir is backed up to the storagebox).
      history = {
        enabled = true;
        "channel-length" = 4096;
        "client-length" = 512;
        "autoresize-window" = "3d";
        "autoreplay-on-join" = 25;
      };
    };
  };

  # ---------------------------------------------------------------------------
  # soju - personal bouncer (tailscale-only), TLS via tailscale cert
  # ---------------------------------------------------------------------------
  tailscaleCertRenew.hostnames = [ tsHost ];
  tailscaleCertRenew.reloadNginx = false;

  users.groups.soju = { };
  users.users.soju = {
    isSystemUser = true;
    group = "soju";
    home = "/var/lib/soju";
  };

  services.soju = {
    enable = true;
    hostName = tsHost;
    listen = [ "ircs://:${toString sojuTlsPort}" ];
    tlsCertificate = "${certDir}/${tsHost}.crt";
    tlsCertificateKey = "${certDir}/${tsHost}.key";
    enableMessageLogging = true;
  };

  systemd.services.soju = {
    after = [ "tailscale-cert-renew.service" "ergochat.service" ];
    wants = [ "tailscale-cert-renew.service" ];
    serviceConfig = {
      DynamicUser = lib.mkForce false;
      User = "soju";
      Group = "soju";
      SupplementaryGroups = [ "nginx" ];
      StateDirectory = "soju";
      RuntimeDirectory = "soju";
      WorkingDirectory = "/var/lib/soju";
    };
  };

  # tailscale-cert-renew creates certDir owned root:nginx; we run no nginx, so
  # define the group and let soju read the cert via its supplementary group.
  users.groups.nginx = { };

  # ---------------------------------------------------------------------------
  # Backups + health
  # ---------------------------------------------------------------------------
  vmBackups = {
    paths = [
      "/var/lib/ergo"
      "/var/lib/soju"
    ];
  };

  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://127.0.0.1:9100/metrics"; }
  ];

  systemd.services.irc-health-check = {
    description = "Check Ergo and soju are listening";
    after = [ "ergochat.service" "soju.service" ];
    serviceConfig = {
      Type = "oneshot";
      ExecStart = pkgs.writeShellScript "irc-health-check" ''
        set -u
        DIR=/var/lib/node-exporter/textfile
        mkdir -p $DIR
        TMP=$(mktemp $DIR/.irc_health.XXXXXX)
        chmod 0644 $TMP
        if ${pkgs.iproute2}/bin/ss -tlnH 'sport = :${toString ergoPlainPort}' | grep -q LISTEN; then
          echo 'ergo_up 1' > $TMP
        else
          echo 'ergo_up 0' > $TMP
        fi
        if ${pkgs.iproute2}/bin/ss -tlnH 'sport = :${toString sojuTlsPort}' | grep -q LISTEN; then
          echo 'soju_up 1' >> $TMP
        else
          echo 'soju_up 0' >> $TMP
        fi
        mv $TMP $DIR/irc_health.prom
      '';
    };
  };

  systemd.timers.irc-health-check = {
    description = "Periodic IRC stack health check";
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnBootSec = "60s";
      OnUnitActiveSec = "1min";
      AccuracySec = "5s";
    };
  };
}
