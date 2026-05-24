{
  hefe,
  pkgs,
  ...
}:

{
  config,
  ...
}:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  # systemd.services.amalthea-backend = {
  #   description = "Amalthea Backend Api";
  #   after = [ "network.target" ];
  #   wantedBy = [ "multi-user.target" ];
  #   serviceConfig = {
  #     ExecStart = "${hefe.users.hanemile.projects.go.amalthea.backend}/bin/amalthea-backend";
  #     Restart = "on-failure";
  #     DynamicUser = true;
  #     StateDirectory = "amalthea";
  #   };
  #   environment =
  #     let
  #       ipam.root = hefe.ops.ipam.default;
  #       ipam.amalthea = ipam.amalthea;
  #       ipam.photo = ipam.photo;

  #       listen.amalthea.host = ipam.amalthea.v4;
  #       listen.amalthea.port = ipam.amalthea.ports.backend;
  #       listen.photo.host = ipam.photo.v4;
  #       listen.photo.port = ipam.photo.ports.immich;
  #     in
  #     {
  #       LISTEN_ADDR = with listen.amalthea; "${host}:${toString port}";
  #       OIDC_ISSUER = "https://sso.emile.space";
  #       OIDC_CLIENT_ID = "amalthea";
  #       OIDC_REDIRECT_URI = "https://amalthea.medano.emile.space/auth/callback";
  #       IMMICH_URL = with listen.photo; "http://${host}:${toString port}";
  #       STATIC_DIR = "${hefe.users.hanemile.projects.go.amalthea.web}";
  #       OIDC_CLIENT_SECRET_FILE = "%d/oidc-secret";
  #       IMMICH_API_KEY_FILE = "%d/immich-key";
  #     };
  # };

  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [
    hefe.ops.ipam.default.amalthea.ports.backend
  ];

  networking.hostName = "amalthea";
  system.stateVersion = "25.05";

  services.healthProbes.probes = [
    { name = "self"; url = "http://${hefe.ops.ipam.default.amalthea.v4}:9100/metrics"; }
  ];
}
