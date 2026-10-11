{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.r2wars-web;
in
{
  options.services.r2wars-web = {
    enable = lib.mkEnableOption "r2wars-web, a radare2 core-war style bot arena";

    # No default: the package lives under //users (code there isn't
    # considered stable/dependable repo-wide - see the usersFilter comment in
    # the root default.nix), so only the allow-listed VM config that reaches
    # into //users is supposed to resolve it, e.g.:
    #   package = hefe.users.hanemile.projects.go.r2wars-web;
    package = lib.mkOption {
      type = lib.types.package;
      description = "The r2wars-web package to run.";
    };

    address = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1";
      description = "Address to listen on.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "Port to listen on.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/r2wars-web";
      description = "Directory holding the sqlite databases, server log and periodic backups.";
    };

    backupInterval = lib.mkOption {
      type = lib.types.str;
      default = "1h";
      description = "How often to snapshot the database (Go duration format, e.g. 1h, 30m). 0 disables backups.";
    };

    backupKeep = lib.mkOption {
      type = lib.types.int;
      default = 24;
      description = "How many database backup snapshots to retain.";
    };

    # SESSION_KEY (required by the binary - it refuses to start without it)
    # and the optional SALT/VERSION env vars live here as plain KEY=VALUE
    # lines, systemd's native EnvironmentFile format. See ../secrets/README.md
    # for how to create/edit the backing agenix secret.
    environmentFile = lib.mkOption {
      type = lib.types.path;
      description = ''
        Path to an EnvironmentFile providing at least SESSION_KEY, e.g. an
        agenix-decrypted secret. Required - the service will fail to start
        without SESSION_KEY set.
      '';
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "r2wars-web";
      description = "User to run the service as.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "r2wars-web";
      description = "Group to run the service as.";
    };

    baseUrl = lib.mkOption {
      type = lib.types.str;
      example = "https://r2wa.rs";
      description = ''
        Public base URL (no trailing slash), used to build the links emailed
        for login. There is no password login to fall back to, so this must
        be the real externally-reachable URL.
      '';
    };

    magicLinkTTL = lib.mkOption {
      type = lib.types.str;
      default = "15m";
      description = "How long an emailed login link stays valid (Go duration format).";
    };

    smtp = {
      host = lib.mkOption {
        type = lib.types.str;
        description = "SMTP server host used to send login-link emails.";
      };

      port = lib.mkOption {
        type = lib.types.port;
        default = 587;
        description = "SMTP server port.";
      };

      username = lib.mkOption {
        type = lib.types.str;
        description = "SMTP auth username.";
      };

      from = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        description = "From address for login-link emails. Defaults to smtp.username.";
      };

      passwordFile = lib.mkOption {
        type = lib.types.path;
        description = "Path to a file containing the SMTP auth password (e.g. an agenix secret).";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      createHome = true;
      description = "r2wars-web service user";
    };
    users.groups.${cfg.group} = { };

    systemd.services.r2wars-web = {
      description = "r2wars-web (radare2 core-war bot arena)";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      # r2/rasm2 must be on PATH - the binary shells out to them per battle
      # run/assembly request rather than linking against libr_core.
      path = [ pkgs.radare2 ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        WorkingDirectory = cfg.dataDir;
        EnvironmentFile = cfg.environmentFile;
        ExecStart = lib.concatStringsSep " " ([
          "${cfg.package}/bin/r2wars-web"
          "-host ${cfg.address}"
          "-port ${toString cfg.port}"
          "-databasepath ${cfg.dataDir}/main.db"
          "-sessiondbpath ${cfg.dataDir}/sessions.db"
          "-logfilepath ${cfg.dataDir}/server.log"
          "-templates ${cfg.package}/share/r2wars-web/templates"
          "-backupdir ${cfg.dataDir}/backups"
          "-backupinterval ${cfg.backupInterval}"
          "-backupkeep ${toString cfg.backupKeep}"
          "-baseurl ${cfg.baseUrl}"
          "-magiclinkttl ${cfg.magicLinkTTL}"
          "-smtphost ${cfg.smtp.host}"
          "-smtpport ${toString cfg.smtp.port}"
          "-smtpusername ${cfg.smtp.username}"
          "-smtppasswordfile ${cfg.smtp.passwordFile}"
        ] ++ lib.optional (cfg.smtp.from != null) "-smtpfrom ${cfg.smtp.from}");

        # Graceful shutdown: the app itself drains in-flight requests (and a
        # running battle) on SIGTERM, so give it real time before SIGKILL.
        TimeoutStopSec = "35s";
        Restart = "on-failure";
        RestartSec = "2s";

        # Hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
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
        ReadWritePaths = [ cfg.dataDir ];
      };
    };
  };
}
