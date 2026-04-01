{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.makhor;
  settingsFormat = pkgs.formats.json { };

  # Generate the config file from the settings
  configFile = settingsFormat.generate "makhor-config.json" cfg.settings;
in
{
  options.services.makhor = {
    enable = lib.mkEnableOption "makhor link aggregator";

    #package = lib.mkPackageOption pkgs "makhor" {
    #  default = [ "makhor" ];
    #};

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ../pkgs/makhor.nix {};
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

    baseUrl = lib.mkOption {
      type = lib.types.str;
      default = "http://localhost:8080";
      description = "Base URL for links (used in emails and redirects).";
    };

    subPath = lib.mkOption {
      type = lib.types.str;
      default = "/";
      description = "Subpath to run on (if any)";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/makhor";
      description = "Directory to store the database and other data.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "makhor";
      description = "User under which makhor runs.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "makhor";
      description = "Group under which makhor runs.";
    };

    enableRss = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Enable RSS feed polling.";
    };

    rssInterval = lib.mkOption {
      type = lib.types.str;
      default = "5m";
      description = "RSS polling interval (Go duration format, e.g., 5m, 1h).";
    };

    createAdmin = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "admin:admin@example.com";
      description = ''
        Create an admin user on startup. Format: "username:email".
        Only creates the user if it doesn't already exist.
      '';
    };

    settings = lib.mkOption {
      type = lib.types.submodule {
        freeformType = settingsFormat.type;

        options = {
          email = lib.mkOption {
            type = lib.types.submodule {
              options = {
                host = lib.mkOption {
                  type = lib.types.str;
                  default = "";
                  description = "SMTP server host.";
                };

                port = lib.mkOption {
                  type = lib.types.port;
                  default = 587;
                  description = "SMTP server port.";
                };

                user = lib.mkOption {
                  type = lib.types.str;
                  default = "";
                  description = "SMTP username.";
                };

                password = lib.mkOption {
                  type = lib.types.str;
                  default = "";
                  description = ''
                    SMTP password. Consider using password_file instead for security.
                  '';
                };

                password_file = lib.mkOption {
                  type = lib.types.nullOr lib.types.path;
                  default = null;
                  description = ''
                    Path to a file containing the SMTP password.
                    This is more secure than putting the password directly in the config.
                  '';
                };

                from = lib.mkOption {
                  type = lib.types.str;
                  default = "";
                  description = "From address for outgoing emails.";
                };

                use_tls = lib.mkOption {
                  type = lib.types.bool;
                  default = true;
                  description = "Use TLS for SMTP connection.";
                };
              };
            };
            default = { };
            description = "Email/SMTP configuration.";
          };
        };
      };
      default = { };
      description = ''
        Configuration for makhor. See the example config.json for available options.
      '';
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Whether to open the firewall port for makhor.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      createHome = true;
      description = "Makhor service user";
    };

    users.groups.${cfg.group} = { };

    systemd.services.makhor = {
      description = "Makhor Link Aggregator";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        WorkingDirectory = cfg.dataDir;
        ExecStart = lib.concatStringsSep " " (
          [
            "${cfg.package}/bin/makhor"
            "-addr=${cfg.address}:${toString cfg.port}"
            "-db=${cfg.dataDir}/makhor.db"
            "-base-url=${cfg.baseUrl}"
            "-enable-rss=${lib.boolToString cfg.enableRss}"
            "-rss-interval=${cfg.rssInterval}"
          ]
          ++ lib.optional (cfg.settings.email.host != "") "-config=${configFile}"
          ++ lib.optional (cfg.createAdmin != null) "-create-admin=${cfg.createAdmin}"
        );
        Restart = "on-failure";
        RestartSec = "5s";

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

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];
  };
}
