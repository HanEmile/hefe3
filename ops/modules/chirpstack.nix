# Self-hosted LoRaWAN network server (ChirpStack v4), colocated with the
# lora8 Semtech UDP (GWMP) packet forwarder that today only talks to TTN.
#
# The pinned nixos-26.05 ships chirpstack-* companion packages
# (chirpstack-gateway-bridge, chirpstack-udp-forwarder, ...) but NOT the
# core `chirpstack` server and there is no `services.chirpstack` NixOS
# module - so the network server itself runs as an OCI container
# (chirpstack/chirpstack:4, podman, host networking) while PostgreSQL,
# Redis, Mosquitto, the Gateway Bridge and the Packet Multiplexer run as
# native NixOS services. Pinning resides in the OCI image tag (option
# `image`); upstream config templates this file follows live at
# https://www.chirpstack.io/docs/chirpstack/configuration.html .
#
# Uplink fan-out (TTN + ChirpStack): lora8 supports exactly one `-fwd`
# target (single UDP socket), so a chirpstack-packet-multiplexer sits in
# front: lora8 -> mux (127.0.0.1:1700) -> TTN (unchanged) + Gateway Bridge
# (127.0.0.1:1701). The multiplexer package only exists in nixos-unstable;
# the caller must pass pkgs-unstable when enabling the multiplexer:
#
#   imports = [
#     (import ../../../modules/chirpstack.nix { inherit hefe pkgs-unstable; })
#   ];
#   services.chirpstack-lns.enable = true;
#   services.lora8.lns = "127.0.0.1:1700"; # was eu1.cloud.thethings.network:1700
#
# Ports used (all loopback-only except the web UI):
#   8090/tcp  ChirpStack web UI + gRPC/REST API  (firewalled to tailscale0)
#   1700/udp  packet multiplexer (GWMP, loopback, lora8's new -fwd target)
#   1701/udp  chirpstack-gateway-bridge semtech_udp backend (loopback)
#   1883/tcp  mosquitto (loopback, region event/command topics)
#   6379/tcp  redis (loopback, NS session/devaddr state)
#   (socket)  /run/postgresql/.s.PGSQL.5432 (mounted into the container)
#
# After deploy: log in to http://<tailscale-ip>:8090 (admin/admin) and
# register the gateway under the default tenant - Gateway EUI = the MCU
# bridge UID shown on the lora8 web UI status page, same EUI as in the TTN
# console. ChirpStack runs its DB migrations (incl. pg_trgm, created by the
# postStart hook below) on first start.

{ hefe, pkgs-unstable ? null }:

{ config, lib, pkgs, ... }:

let
  cfg = config.services.chirpstack-lns;

  pgPkg = config.services.postgresql.package;

  # --- chirpstack-gateway-bridge (Semtech UDP 1701/udp -> MQTT 1883) ---
  gatewayBridgeConfig = pkgs.writeText "chirpstack-gateway-bridge.toml" ''
    [general]
      log_level = 4

    [backend]
      type = "semtech_udp"

      [backend.semtech_udp]
        udp_bind = "127.0.0.1:${toString cfg.gatewayBridge.port}"
        fake_rx_time = false

    [integration]
      marshaler = "protobuf"

      [integration.mqtt]
        event_topic_template = "${cfg.region}/gateway/{{ .GatewayID }}/event/{{ .EventType }}"
        state_topic_template = "${cfg.region}/gateway/{{ .GatewayID }}/state/{{ .StateType }}"
        command_topic_template = "${cfg.region}/gateway/{{ .GatewayID }}/command/#"

        [integration.mqtt.auth]
          type = "generic"

          [integration.mqtt.auth.generic]
            servers = [ "tcp://127.0.0.1:1883" ]
            clean_session = true
  '';

  # --- chirpstack-packet-multiplexer (one lora8 GWMP stream -> N LNS) ---
  multiplexerConfig = pkgs.writeText "chirpstack-packet-multiplexer.toml" ''
    [logging]
      level = "info"

    [multiplexer]
      bind = "${cfg.multiplexer.bind}"

      # Existing TTN EU uplink (uplink+downlink, behaviour unchanged).
      ${lib.concatMapStringsSep "\n" (s: ''
        [[multiplexer.server]]
          server = "${s.server}"
          uplink_only = ${lib.boolToString s.uplinkOnly}'') cfg.multiplexer.servers}

      # Local ChirpStack Gateway Bridge.
      [[multiplexer.server]]
        server = "127.0.0.1:${toString cfg.gatewayBridge.port}"
        uplink_only = ${lib.boolToString cfg.multiplexer.chirpstackUplinkOnly}
  '';

  # --- chirpstack server config (rendered into the container) ---
  # @API_SECRET@ is substituted at preStart from /var/lib/chirpstack/.api-secret
  # (generated on first boot, stays out of the nix store).
  chirpstackToml = pkgs.writeText "chirpstack.toml" ''
    [logging]
      level = "debug"

    # Native NixOS PostgreSQL via the unix socket bind-mounted from the host.
    # Peer auth maps container root -> db user chirpstack (identMap below).
    [postgresql]
      dsn = "postgres://chirpstack@/chirpstack?sslmode=disable&host=/run/postgresql"
      max_open_connections = 10

    [redis]
      servers = [ "redis://127.0.0.1:6379/" ]

    # v4 serves web UI + gRPC + REST on a single port.
    [api]
      bind = "0.0.0.0:${toString cfg.web.port}"
      secret = "@API_SECRET@"

    [gateway]
      allow_unknown_gateways = false

    [network]
      net_id = "${cfg.netId}"
      enabled_regions = [ "${cfg.region}" ]
  '';

  regionToml = pkgs.writeText "region_${cfg.region}.toml" ''
    [[regions]]
      name = "${cfg.region}"
      common_name = "EU868"

      [regions.gateway]
        force_gws_private = false

        [regions.gateway.backend]
          enabled = "mqtt"

          [regions.gateway.backend.mqtt]
            event_topic = "${cfg.region}/gateway/+/event/+"
            command_topic = "${cfg.region}/gateway/{{ gateway_id }}/command/{{ command }}"
            server = "tcp://127.0.0.1:1883"
            clean_session = true

        # Concentratord channel list of the stock EU868 config; ignored by
        # the semtech_udp backend, kept so the region file stays complete.
        [[regions.gateway.channels]]
          frequency = 868100000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 868300000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 868500000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 867100000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 867300000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 867500000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 867700000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 867900000
          bandwidth = 125000
          modulation = "LORA"
          spreading_factors = [ 7, 8, 9, 10, 11, 12 ]
        [[regions.gateway.channels]]
          frequency = 868300000
          bandwidth = 250000
          modulation = "LORA"
          spreading_factors = [ 7 ]
        [[regions.gateway.channels]]
          frequency = 868800000
          bandwidth = 125000
          modulation = "FSK"
          datarate = 50000

      [regions.network]
        installation_margin = 10
        rx_window = 0
        rx1_delay = 1
        rx1_dr_offset = 0
        rx2_dr = 0
        rx2_frequency = 869525000
        downlink_tx_power = -1
        adr_disabled = false
        min_dr = 0
        max_dr = 5

        # Standard EU868 CFList channels.
        [[regions.network.extra_channels]]
          frequency = 867100000
          min_dr = 0
          max_dr = 5
        [[regions.network.extra_channels]]
          frequency = 867300000
          min_dr = 0
          max_dr = 5
        [[regions.network.extra_channels]]
          frequency = 867500000
          min_dr = 0
          max_dr = 5
        [[regions.network.extra_channels]]
          frequency = 867700000
          min_dr = 0
          max_dr = 5
        [[regions.network.extra_channels]]
          frequency = 867900000
          min_dr = 0
          max_dr = 5
  '';
in
{
  options.services.chirpstack-lns = {
    enable = lib.mkEnableOption "self-hosted ChirpStack LoRaWAN network server (alongside the lora8 TTN uplink)";

    image = lib.mkOption {
      type = lib.types.str;
      default = "docker.io/chirpstack/chirpstack:4";
      description = "OCI image of the ChirpStack network server (the only piece not available as a nixpkgs package).";
    };

    region = lib.mkOption {
      type = lib.types.str;
      default = "eu868";
      description = "ChirpStack region name; also the MQTT topic prefix shared with the gateway bridge.";
    };

    netId = lib.mkOption {
      type = lib.types.str;
      default = "000000";
      description = "LoRaWAN NetID (3 bytes, HEX) for this private network.";
    };

    web.port = lib.mkOption {
      type = lib.types.port;
      default = 8090;
      description = "Web UI / gRPC / REST port (8080 is already taken by the lora8 UI). Exposed on tailscale0 only.";
    };

    gatewayBridge.port = lib.mkOption {
      type = lib.types.port;
      default = 1701;
      description = "Loopback Semtech UDP (GWMP) port of chirpstack-gateway-bridge.";
    };

    multiplexer = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Run chirpstack-packet-multiplexer in front of both LNSes. lora8
          supports only a single -fwd target; point services.lora8.lns at
          cfg.bind (127.0.0.1:1700) to keep TTN AND ChirpStack fed.
          Requires the caller to pass pkgs-unstable to this module.
        '';
      };

      bind = lib.mkOption {
        type = lib.types.str;
        default = "127.0.0.1:1700";
        description = "interface:port the multiplexer receives GWMP data on (lora8's forward target).";
      };

      servers = lib.mkOption {
        type = lib.types.listOf (lib.types.submodule {
          options = {
            server = lib.mkOption { type = lib.types.str; };
            uplinkOnly = lib.mkOption {
              type = lib.types.bool;
              default = false;
              description = "If true, downlinks from this server are discarded.";
            };
          };
        });
        default = [
          {
            server = "eu1.cloud.thethings.network:1700";
            uplinkOnly = false;
          }
        ];
        description = "Upstream LNSes besides the local ChirpStack (default: the existing TTN EU uplink).";
      };

      chirpstackUplinkOnly = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Discard downlinks coming from the local ChirpStack (set true to leave TTN sole owner of downlink scheduling).";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.multiplexer.enable -> pkgs-unstable != null;
        message = "services.chirpstack-lns: the multiplexer needs chirpstack-packet-multiplexer from pkgs-unstable - import this module with { inherit hefe pkgs-unstable; } (or set multiplexer.enable = false).";
      }
    ];

    # ---- backing services (system-native) ----

    services.postgresql = {
      enable = lib.mkDefault true;
      ensureDatabases = [ "chirpstack" ];
      ensureUsers = [
        {
          name = "chirpstack";
          ensureDBOwnership = true;
        }
      ];
      # peer auth over the unix socket: the OCI container runs as root
      # (rootful podman, host net + /run/postgresql bind-mounted).
      identMap = ''
        chirpstack-lns chirpstack chirpstack
        chirpstack-lns root chirpstack
        # the OCI image runs as nobody:nogroup (uid 65534); peer-auth it
        # into the chirpstack role the same way as root.
        chirpstack-lns nobody chirpstack
      '';
      authentication = lib.mkBefore ''
        local chirpstack chirpstack peer map=chirpstack-lns
      '';
    };

    # ChirpStack migrations require the pg_trgm extension. NixOS 26.05 runs
    # ensureDatabases/ensureUsers from the dedicated postgresql-setup unit,
    # so attach there — postgresql.service's own postStart fires before any
    # database exists. (Chained after the unit's own ensure steps.)
    systemd.services.postgresql-setup.postStart = lib.mkAfter ''
      ${pgPkg}/bin/psql -d chirpstack -tAc 'CREATE EXTENSION IF NOT EXISTS pg_trgm'
    '';

    services.redis.servers.chirpstack = {
      enable = true;
      bind = "127.0.0.1";
      port = 6379;
    };

    services.mosquitto = {
      enable = true;
      logType = [ "debug" "error" "warning" "notice" "information" "subscribe" "unsubscribe" ];
      listeners = [
        {
          # Single LAN-visible listener (localhost services use it too):
          # a separate 127.0.0.1 listener on the same port cannot coexist
          # in one broker instance. The reason we added a public listener at
          # all: mesh nodes (meshtastic/mqtt WiFi module) need to reach it.
          address = "0.0.0.0";
          port = 1883;
          settings.listener_allow_anonymous = true;
          # security relies on loopback + firewall only.
          #
          # Trap caught while bringging dns up: mosquito mounts the
          # acl_file.so plugin per listener, and an EMPTY acl file means
          # *anonymous publishes are silently denied* — that's precisely
          # why chirpstack saw zero uplinks once, with no log telling us why.
          acl = [ "pattern readwrite #" ];
        }
      ];
    };

    # ---- chirpstack-pack-multiplexer + gateway bridge (native, systemd) ----

    systemd.services.chirpstack-packet-multiplexer = lib.mkIf cfg.multiplexer.enable {
      description = "ChirpStack UDP Packet Multiplexer (lora8 -> TTN + ChirpStack GWMP fan-out)";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        ExecStart = "${pkgs-unstable.chirpstack-packet-multiplexer}/bin/chirpstack-packet-multiplexer --config ${multiplexerConfig}";
        Restart = "on-failure";
        RestartSec = "5s";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
      };
    };

    systemd.services.chirpstack-gateway-bridge = {
      description = "ChirpStack Gateway Bridge (semtech_udp :${toString cfg.gatewayBridge.port} -> MQTT for region ${cfg.region})";
      wantedBy = [ "multi-user.target" ];
      after = [
        "mosquitto.service"
        "network-online.target"
      ];
      wants = [
        "mosquitto.service"
        "network-online.target"
      ];
      serviceConfig = {
        Type = "simple";
        DynamicUser = true;
        ExecStart = "${pkgs.chirpstack-gateway-bridge}/bin/chirpstack-gateway-bridge --config ${gatewayBridgeConfig}";
        Restart = "on-failure";
        RestartSec = "5s";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
      };
    };

    # REST gateway for the gRPC chirpstack API (the podman server itself
    # only serves UI + gRPC-web; JSON REST automation goes through this
    # sidecar, loopback-only).
    systemd.services.chirpstack-rest-api = {
      description = "ChirpStack REST API frontend (JSON REST -> gRPC for the chirpstack server)";
      wantedBy = [ "multi-user.target" ];
      after = [ "podman-chirpstack.service" ];
      wants = [ "podman-chirpstack.service" ];
      serviceConfig = {
        Type = "simple";
        ExecStart = "${pkgs.chirpstack-rest-api}/bin/chirpstack-rest-api -server 127.0.0.1:${toString cfg.web.port} -bind 127.0.0.1:8091 -insecure";
        Restart = "on-failure";
        RestartSec = "5s";
        DynamicUser = true;
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        # needs to read the JWT secret the podman service renders
        ReadOnlyPaths = [ "/var/lib/chirpstack/.api-secret" ];
      };
    };

    # ---- chirpstack server (OCI container; no nixpkgs package exists) ----

    virtualisation.podman.enable = true;
    virtualisation.oci-containers.backend = "podman";
    virtualisation.oci-containers.containers.chirpstack = {
      inherit (cfg) image;
      cmd = [
        "-c"
        "/etc/chirpstack"
      ];
      extraOptions = [
        # Shares the host net stack: reaches postgres (socket below),
        # redis 6379 and mosquitto 1883 on 127.0.0.1; the firewall then
        # gates the web port per-interface (tailscale0 only).
        "--network=host"
      ];
      volumes = [
        "/var/lib/chirpstack/config:/etc/chirpstack:ro"
        "/run/postgresql:/run/postgresql"
      ];
    };

    systemd.services."podman-chirpstack" = {
      after = [
        "postgresql.service"
        "redis-chirpstack.service"
        "mosquitto.service"
      ];
      wants = [
        "postgresql.service"
        "redis-chirpstack.service"
        "mosquitto.service"
      ];
      # Render the config dir (secret excluded from the nix store) and wait
      # for the backing services, mkBefore so image-pull/rm come after.
      preStart = with lib; mkBefore ''
        set -e
        # the OCI image runs as nobody:nogroup; the config dir (rendered
        # chirpstack.toml, symlinked region file) must be traversable by it.
        # The JWT secret itself never lands here — only under .api-secret
        # (root 0600), used by sed at preStart.
        install -d -m 0755 /var/lib/chirpstack/config
        if [ ! -f /var/lib/chirpstack/.api-secret ]; then
          (umask 077; ${pkgs.openssl}/bin/openssl rand -hex 32 > /var/lib/chirpstack/.api-secret)
        fi
        ${pkgs.gnused}/bin/sed "s|@API_SECRET@|$(cat /var/lib/chirpstack/.api-secret)|" \
          ${chirpstackToml} > /var/lib/chirpstack/config/chirpstack.toml
        # regular file, not a symlink: the target lives in the nix store,
        # which is NOT mounted into the container.
        install -m 0644 -T ${regionToml} /var/lib/chirpstack/config/region_${cfg.region}.toml

        # postgres is "started" once postStart (ensure* + pg_trgm) has run,
        # but be defensive about the socket actually accepting.
        for _ in $(seq 1 30); do
          ${pgPkg}/bin/pg_isready -q -h /run/postgresql && break
          sleep 1
        done
      '';
    };

    # Web UI on tailnet only (same pattern as the node-exporter 9100 rule).
    networking.firewall.interfaces.tailscale0.allowedTCPPorts = [ cfg.web.port ];
    # Mesh nodes on the LAN publish over MQTT into this broker.
    networking.firewall.allowedTCPPorts = [ 1883 ];
  };
}
