{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.late-sh;
  dbName = cfg.database.name;
in
{
  options.services.late-sh = {
    enable = lib.mkEnableOption "late-sh social SSH terminal";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.late-sh;
      description = "The late-sh derivation to run.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "late_sh";
      description = "User under which late-sh runs.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "late_sh";
      description = "Group under which late-sh runs.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/late-sh";
      description = "Directory to store keys, dynamic states, and database tracks.";
    };

    database = {
      createLocally = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Whether to set up a local PostgreSQL instance automatically.";
      };
      name = lib.mkOption {
        type = lib.types.str;
        default = "late_sh";
        description = "Database name.";
      };
    };

    # Secret Management Paths (supports sops-nix / agenix EnvironmentFiles)
    secretFiles = lib.mkOption {
      type = lib.types.listOf lib.types.path;
      default = [ ];
      description = ''
        A list of paths to encrypted environment files containing sensitive variables
        (e.g., LATE_AI_API_KEY, LATE_WEB_TUNNEL_TOKEN, LATE_FILES_S3_SECRET_ACCESS_KEY).
      '';
    };

    # Server Ports & External Routings
    port = lib.mkOption {
      type = lib.types.port;
      default = 2222;
      description = "SSH service inbound connection port.";
    };

    apiPort = lib.mkOption {
      type = lib.types.port;
      default = 4001;
      description = "Internal HTTP API infrastructure listen port.";
    };

    webUrl = lib.mkOption {
      type = lib.types.str;
      default = "http://localhost:3000";
      description = "Public core platform entry point web URL.";
    };

    # Media Stream Forwarders
    icecastUrl = lib.mkOption {
      type = lib.types.str;
      default = "http://127.0.0.1:8001";
      description = "Icecast stream server cluster address destination.";
    };

    liquidsoapAddr = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:1234";
      description = "Liquidsoap automation control loop telnet target socket.";
    };

    # Network Security Proxies & Validation Pools
    proxy = {
      enableProtocol = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Parse inbound HAProxy/Nginx proxy protocol header frames.";
      };
      trustedCidrs = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        description = "Comma-separated trusted upstream CIDR blocks parsing proxy structures.";
      };
    };

    openAccess = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Allow inbound connections to connect without cryptographic matches up front.";
    };

    forceAdmin = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Forced administrative bypass levels across initialization boundaries.";
    };
  };

  config = lib.mkIf cfg.enable {

    # Setup database system automatically if flagged true
    services.postgresql = lib.mkIf cfg.database.createLocally {
      enable = true;
      package = pkgs.postgresql_18;
      ensureDatabases = [ dbName ];
      ensureUsers = [
        {
          name = cfg.user;
          ensureDBOwnership = true;
        }
      ];
    };

    services.icecast = {
      enable = true;
      hostname = "127.0.0.1"; # Fixes the 'cannot coerce null to a string' crash!
      listen.port = 8001;
      listen.address = "127.0.0.1";

      admin.user = "admin";
      admin.password = "hackme";

      # Inject custom configurations cleanly through the official extraConf hook
      extraConf = ''
        <limits>
            <clients>100</clients>
            <sources>2</sources>
            <queue-size>524288</queue-size>
            <client-timeout>30</client-timeout>
            <header-timeout>15</header-timeout>
            <source-timeout>10</source-timeout>
            <burst-on-connect>1</burst-on-connect>
            <burst-size>65535</burst-size>
        </limits>
        <authentication>
            <source-password>hackme</source-password>
            <relay-password>hackme</relay-password>
            <admin-user>admin</admin-user>
            <admin-password>hackme</admin-password>
        </authentication>
        <http-headers>
            <header name="Access-Control-Allow-Origin" value="*" />
        </http-headers>
      '';
    };

    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      createHome = true;
      extraGroups = [ "postgresql" ];
      description = "late-sh service execution runtime context user";
    };

    users.groups.${cfg.group} = { };

    systemd.services.late-sh = {
      description = "late-sh social SSH terminal engine service core";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ] ++ lib.optional cfg.database.createLocally "postgresql.service";
      requires = lib.optional cfg.database.createLocally "postgresql.service";

      environment = {
        # Network Endpoint Parameters
        LATE_SSH_PORT = toString cfg.port;
        LATE_API_PORT = toString cfg.apiPort;
        LATE_WEB_URL = cfg.webUrl;
        LATE_ALLOWED_ORIGINS = "${cfg.webUrl},http://localhost:3000";
        LATE_SSH_KEY_PATH = "${cfg.dataDir}/server_key";

        # Database Pipeline Connections
        LATE_DB_HOST = "/run/postgresql";
        LATE_DB_PORT = "5432";
        LATE_DB_USER = cfg.user; # Connecting as the database owner user via Unix Sockets
        LATE_DB_NAME = cfg.database.name;
        LATE_DB_PASSWORD = "peer_authenticated_socket"; # Strict non-empty validator fallback pass
        LATE_DB_POOL_SIZE = "16";

        # Service Internal Constants & Sizing Caps
        LATE_SSH_OPEN = if cfg.openAccess then "1" else "0";
        LATE_FORCE_ADMIN = if cfg.forceAdmin then "1" else "0";
        LATE_MAX_CONNS_GLOBAL = "10000";
        LATE_MAX_CONNS_PER_IP = "3";
        LATE_SSH_IDLE_TIMEOUT = "3600";
        LATE_FRAME_DROP_LOG_EVERY = "100";
        LATE_VOTE_SWITCH_INTERVAL_SECS = "3600";

        # Rate Limit Core Throttling Parameters
        LATE_SSH_MAX_ATTEMPTS_PER_IP = "30";
        LATE_SSH_RATE_LIMIT_WINDOW_SECS = "60";
        LATE_WS_PAIR_MAX_ATTEMPTS_PER_IP = "30";
        LATE_WS_PAIR_RATE_LIMIT_WINDOW_SECS = "60";

        # Network Security Proxies Layouts
        LATE_SSH_PROXY_PROTOCOL = if cfg.proxy.enableProtocol then "1" else "0";
        LATE_SSH_PROXY_TRUSTED_CIDRS = lib.concatStringsSep "," cfg.proxy.trustedCidrs;

        # Multimedia Backend Links
        LATE_ICECAST_URL = cfg.icecastUrl;
        LATE_LIQUIDSOAP_ADDR = cfg.liquidsoapAddr;

        # Intelligent Execution Runtimes Defaults
        LATE_AI_ENABLED = "0";
        LATE_AI_MODEL = "gemini-3.1-pro-preview";
        LATE_AI_API_KEY = "123456789"; # it's off, but a key is required, idk why

        LATE_WEB_TUNNEL_TOKEN = "123456789";
      };

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;

        StateDirectory = "late_sh";
        WorkingDirectory = cfg.dataDir;

        # Injects your list of age/sops files seamlessly right inside the engine context
        EnvironmentFile = cfg.secretFiles;

        ExecStartPre = pkgs.writeShellScript "late-sh-setup" ''
          if [ ! -f "${cfg.dataDir}/server_key" ]; then
            ${pkgs.openssh}/bin/ssh-keygen -t ed25519 -f "${cfg.dataDir}/server_key" -N "" -q
            chown ${cfg.user}:${cfg.group} "${cfg.dataDir}/server_key" "${cfg.dataDir}/server_key.pub"
          fi
        '';

        ExecStart = "${cfg.package}/bin/late-ssh";
        Restart = "on-failure";
        RestartSec = "5s";

        # Hardening Container Isolations
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

        AmbientCapabilities = [ "CAP_NET_BIND_SERVICE" ];
        CapabilityBoundingSet = [ "CAP_NET_BIND_SERVICE" ];

        # Filesystem Bind Mappings
        BindReadOnlyPaths = [ "/run/postgresql" ];
        ReadWritePaths = [ cfg.dataDir ];
      };
    };
  };
}
