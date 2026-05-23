{ hefe, pkgs, ... }:
{ ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];


  services.nginx = {
    enable = true;
    virtualHosts."git.pinto-pike.ts.net" = {
      basicAuth = {
        "root" = "derwegglerwegglit";
      };
      listen = [
        { addr = "127.0.0.1"; port = 8080; ssl = false; }
      ];
      locations = {
        "/" = {
          proxyPass = "http://127.0.0.1:8081";
        };
      };
    };
  };

  networking.hostName = "git";
  system.stateVersion = "25.05";

  services.healthProbes.probes = [
    { name = "self"; url = "http://127.0.0.1:8080/"; expectedStatus = 401; }
    { name = "node-exporter"; url = "http://${hefe.ops.ipam.default.git.v4}:9100/metrics"; }
  ];
}
