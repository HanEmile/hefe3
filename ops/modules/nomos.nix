{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.nomos;
  tomlFormat = pkgs.formats.toml { };

  # nomos decodes nomos.toml over its built-in defaults, so only values that
  # differ need to be written (see "Configuration" in the nomos AGENTS.md).
  configFile = tomlFormat.generate "nomos.toml" (
    lib.recursiveUpdate cfg.settings (
      {
        transport.endpoint = cfg.ipfs.endpoint;
      }
      // lib.optionalAttrs (cfg.adminTA != null) {
        verifier.admin_ta = cfg.adminTA;
      }
    )
  );
in
{
  options.services.nomos = {
    enable = lib.mkEnableOption "the nomos NOMOI verifier service";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ../pkgs/nomos { };
      description = "The nomos package to use.";
    };

    bind = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:3000";
      description = ''
        Address to listen on. nomos serve has no authentication and performs
        fetches on behalf of callers — keep it behind a reverse proxy or on
        localhost, never directly exposed.
      '';
    };

    adminTA = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "k51qzi5uqu5dkci...";
      description = ''
        Discovery identifier of the administrator Trust Anchor
        (verifier.admin_ta), bare — k51..., without the /ipns/ prefix. Until
        this is set, verification requests are refused rather than answered.
      '';
    };

    settings = lib.mkOption {
      type = tomlFormat.type;
      default = { };
      description = ''
        Extra nomos.toml content, merged below the explicit options above.
        Use for timeouts (transport.request_timeout etc.) or verifier tuning;
        unset keys keep the nomos built-in defaults.
      '';
    };

    ipfs = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Run a local Kubo daemon for nomos to resolve and fetch through.
        '';
      };

      endpoint = lib.mkOption {
        type = lib.types.str;
        default = "http://127.0.0.1:5001";
        description = ''
          IPFS RPC API endpoint (the RPC API, not the read-only gateway).
        '';
      };

      announce = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        example = [ "/ip4/203.0.113.10/tcp/4001" ];
        description = ''
          Public swarm multiaddrs kubo announces to the DHT. Set when the
          daemon sits behind NAT with the swarm port forwarded: without a
          dialable address, remote verifiers rely on flaky circuit relays to
          fetch published Trust Anchor state.
        '';
      };
    };

    platform = {
      enable = lib.mkEnableOption "the nomos platform appliance (WebUI + management API)";

      package = lib.mkOption {
        type = lib.types.package;
        default = pkgs.callPackage ../pkgs/nomos-platform { };
        description = "The nomos-platform package (feature/platform branch) to use.";
      };

      bind = lib.mkOption {
        type = lib.types.str;
        default = "127.0.0.1:8080";
        description = ''
          Address the appliance binds. Upstream requires an HTTPS reverse
          proxy in front for any remote access; localhost + an SSH forward is
          the loopback-safe default.
        '';
      };

      publicOrigin = lib.mkOption {
        type = lib.types.str;
        default = "http://127.0.0.1:8080";
        example = "https://nomos.emile.space";
        description = ''
          Exact browser origin of the appliance. Forwarded headers never
          select the origin or change cookie security, so this must match what
          operators actually type into their browser, including the scheme.
        '';
      };

      managementPrefix = lib.mkOption {
        type = lib.types.str;
        default = "nomoi";
        description = "Scope prefix under which the appliance names its managed TAs.";
      };

      bootstrapPasswordFile = lib.mkOption {
        type = lib.types.nullOr lib.types.path;
        default = null;
        example = "/run/agenix/nomos_platform_bootstrap_password";
        description = ''
          File whose content (a single line
          NOMOS_PLATFORM_BOOTSTRAP_PASSWORD=<pw>) is loaded via systemd
          EnvironmentFile. The bootstrap password gates operator enrollment
          and is permanently disabled after the first operator NOMOI login.
          Keep the default ("nomos") only on loopback-only instances.
        '';
      };

      extraFlags = lib.mkOption {
        type = lib.types.attrsOf lib.types.str;
        default = { };
        example = { passport-event = "40c3"; };
        description = ''
          Additional NOMOS_PLATFORM_* flags for `nomos platform serve`
          (passport configuration etc.), named as the CLI flag.
        '';
      };
    };
  };

  config = lib.mkIf cfg.enable {
    services.kubo = lib.mkIf cfg.ipfs.enable {
      enable = true;
      # The nixpkgs default exposes the RPC API on /unix/run/ipfs.sock only;
      # nomos talks HTTP, so add the TCP listener its endpoint points at
      # (keeping the socket for local tooling).
      settings.Addresses.API = [
        "/unix/run/ipfs.sock"
        "/ip4/127.0.0.1/tcp/5001"
      ];
      settings.Addresses.Announce = cfg.ipfs.announce;
    };

    systemd.services.nomos = {
      description = "nomos NOMOI verifier";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ] ++ lib.optional cfg.ipfs.enable "ipfs.service";
      wants = lib.optional cfg.ipfs.enable "ipfs.service";

      # nomos requires XDG dirs it can write (it creates ids/, tas/, nonces/
      # at startup even for a verify-only server).
      environment = {
        XDG_CONFIG_HOME = "%S/nomos/config";
        XDG_DATA_HOME = "%S/nomos/data";
        XDG_CACHE_HOME = "%C/nomos";
      };

      preStart = ''
        install -d -m 0755 "$STATE_DIRECTORY/config/nomos"
        install -m 0644 ${configFile} "$STATE_DIRECTORY/config/nomos/nomos.toml"
      '';

      serviceConfig = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/nomos serve --bind ${cfg.bind}";
        Restart = "on-failure";
        RestartSec = "5s";

        DynamicUser = true;
        StateDirectory = "nomos";
        CacheDirectory = "nomos";

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
      };
    };

    systemd.services.nomos-platform = lib.mkIf cfg.platform.enable {
      description = "nomos platform appliance";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ] ++ lib.optional cfg.ipfs.enable "ipfs.service";
      wants = lib.optional cfg.ipfs.enable "ipfs.service";

      serviceConfig = {
        Type = "simple";
        ExecStart =
          let
            extra = lib.mapAttrsToList (n: v: "--${n} ${lib.escapeShellArg v}") cfg.platform.extraFlags;
          in
          lib.concatStringsSep " " (
            [
              "${cfg.platform.package}/bin/nomos platform serve"
              "--bind ${cfg.platform.bind}"
              "--data-dir %S/nomos-platform"
              "--public-origin ${cfg.platform.publicOrigin}"
              "--management-prefix ${cfg.platform.managementPrefix}"
              "--kubo-api ${cfg.ipfs.endpoint}"
            ]
            ++ extra
          );
        Restart = "on-failure";
        RestartSec = "5s";

        DynamicUser = true;
        StateDirectory = "nomos-platform";
        EnvironmentFile = lib.optional (cfg.platform.bootstrapPasswordFile != null) (
          toString cfg.platform.bootstrapPasswordFile
        );

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
      };
    };
  };
}
