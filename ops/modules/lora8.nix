{ hefe }:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.lora8;
in
{
  options.services.lora8 = {
    enable = lib.mkEnableOption "lora8, toolkit + web UI for the MikroTik R11e-LR8 LoRa concentrator";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage hefe.ops.pkgs.lora8 { };
      description = "The lora8 package to run.";
    };

    device = lib.mkOption {
      type = lib.types.str;
      default = "/dev/lora8";
      description = "Serial device of the R11e-LR8 (/dev/lora8 is a udev symlink to /dev/ttyACM*).";
    };

    address = lib.mkOption {
      type = lib.types.str;
      default = "0.0.0.0";
      description = "Address for the web UI to listen on.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "Port for the web UI.";
    };

    lns = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "eu1.cloud.thethings.network:1700";
      description = ''
        Optional LoRaWAN network server (Semtech UDP) to forward packets to.
        The gateway EUI for the LNS registration is the card's MCU bridge UID
        shown on the web UI status page.
      '';
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Open the web UI port in the firewall.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "lora8";
      description = "User to run the service as.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "lora8";
      description = "Group to run the service as.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      extraGroups = [ "dialout" ]; # R/W on /dev/ttyACM* (/dev/lora8)
      description = "lora8 service user";
    };
    users.groups.${cfg.group} = { };

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];

    systemd.services.lora8 = {
      description = "lora8 LoRa concentrator toolkit (R11e-LR8/SX1301)";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        SupplementaryGroups = [ "dialout" ];
        ExecStart = lib.concatStringsSep " " ([
          "${cfg.package}/bin/lora8"
          "-dev ${cfg.device}"
          "web -auto"
          "-addr ${cfg.address}:${toString cfg.port}"
          "-logdir /var/lib/lora8"
        ] ++ lib.optional (cfg.lns != null) "-fwd ${cfg.lns}");

        StateDirectory = "lora8"; # persistent frame history (daily JSONL)

        # The MCU bridge drops off USB for ~20 s whenever it is reset (and on
        # replug), so recover instead of giving up.
        Restart = "always";
        RestartSec = "10s";

        # Hardening (device access to the concentrator must stay).
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictSUIDSGID = true;
        RestrictNamespaces = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        RestrictRealtime = true;
        SystemCallFilter = [ "@system-service" ];
        SystemCallErrorNumber = "EPERM";
        PrivateDevices = false; # needs /dev/ttyACM*/lora8
      };
    };
  };
}
