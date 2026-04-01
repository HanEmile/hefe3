# SSH Router - exports package and module via readTree
{
  pkgs,
  lib ? pkgs.lib,
  ...
}:

let
  # The SSH Router package
  package = pkgs.buildGoModule {
    pname = "sshrouter";
    version = "0.1.0";
    src = lib.cleanSource ./.;
    vendorHash = "sha256-odfpmBQLhJ5gc8RdAQFKT+AQXP1+n845EhOZ8lur+dI=";
  };

  # NixOS module for the SSH router service
  module =
    { config, lib, ... }:
    with lib;
    let
      cfg = config.services.sshrouter;
      routesJson = pkgs.writeText "sshrouter-routes.json" (
        builtins.toJSON {
          listen_addr = "${cfg.listenHost}:${toString cfg.listenPort}";
          host_key_path = cfg.hostKeyPath;
          private_key_path = cfg.privateKeyPath; # Add this
          routes = cfg.routes;
          default = cfg.default;
        }
      );
    in
    {
      options.services.sshrouter = {
        enable = mkEnableOption "SSH Router";
        listenHost = mkOption {
          type = types.str;
          default = "0.0.0.0";
        };
        listenPort = mkOption {
          type = types.int;
          default = 22;
        };
        hostKeyPath = mkOption {
          type = types.path;
          default = "/etc/ssh/ssh_host_ed25519_key";
        };
        privateKeyPath = mkOption { # Add this option
          type = types.path;
          default = "/etc/ssh/ssh_host_ed25519_key";
        };
        routes = mkOption {
          type = types.attrsOf types.str;
          default = {
            "root" = "127.0.0.1:2222";
          };
        };
        default = mkOption {
          type = types.nullOr types.str;
          default = null;
        };
      };

      config = mkIf cfg.enable {
        systemd.services.sshrouter = {
          description = "SSH Router";
          after = [ "network.target" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            ExecStart = "${package}/bin/sshrouter -config ${routesJson}";
            Restart = "on-failure";
          };
        };
        networking.firewall.allowedTCPPorts = [ cfg.listenPort ];
      };
    };
in
{
  inherit package module;
}