{ config, pkgs, ... }:

{
  systemd.services.immich-tailscale-service = {
    description = "Advertise immich as the svc:immich Tailscale service";
    wantedBy = [ "multi-user.target" ];
    after = [ "tailscaled.service" "immich-server.service" ];
    requires = [ "tailscaled.service" ];
    path = [ pkgs.tailscale ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    script = let
      host = config.services.immich.host;
      port = config.services.immich.port;
    in ''
      set +e
      for i in $(seq 1 30); do
        tailscale status >/dev/null 2>&1 && break
        sleep 2
      done
      tailscale serve --service=svc:immich --bg --https=443 ${host}:${toString port}
      exit 0
    '';
  };
}
