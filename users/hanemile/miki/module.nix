{ config, lib, ... }:

let
  cfg = config.services.emile.r2wars-web;
in
with lib;
{
  options.services.emile.miki = {
    enable = mkEnableOption "Enable miki";

    # ip and port to listen on
    host = mkOption {
      type = types.str;
      default = "127.0.0.1";
      example = "0.0.0.0";
      description = "The host the service listens on";
    };

    port = mkOption {
      type = types.int;
      default = 8080;
      example = 8080;
      description = "The port the service listens on";
    };
  };

  config = mkIf cfg.enable {
    systemd.services.miki = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        RestartSec = 5;
        Restart = "always";
      };
      # environment = {
        # SESSION_KEY = cfg.sessionKey;
        # SALT = cfg.salt;
        # VERSION = pkgs.r2wars-web.version;
      # };
      path = [ pkgs.miki ];
      serviceConfig.ExecStart = "${pkgs.miki}/bin/miki -h ${cfg.host} -p ${toString cfg.port}";
    };
  };
}
