# status-board — internal dashboard for the medano fleet.
{ pkgs, lib ? pkgs.lib, ... }:

let
  package = pkgs.buildGoModule {
    pname = "status-board";
    version = "0.1.0";
    src = lib.cleanSource ./.;
    vendorHash = null; # no third-party deps yet
  };

  module = { hefe }:
    { config, lib, pkgs, ... }:
    let
      ipam = hefe.ops.ipam;
      defaultVms = lib.attrNames (lib.filterAttrs (n: _: n != "medano") ipam.default);
      privateVms = lib.attrNames (lib.filterAttrs (n: _: n != "medano") ipam.private);
      allVms = defaultVms ++ privateVms;
      ipamLookup = name:
        if ipam.default ? "${name}" then ipam.default."${name}"
        else if ipam.private ? "${name}" then ipam.private."${name}"
        else throw "status-board: no IPAM for ${name}";
      vmInventory = pkgs.writeText "vm-inventory.json" (builtins.toJSON (
        map (n: {
          name = n;
          ip = (ipamLookup n).v4;
          bridge = if ipam.default ? "${n}" then "default" else "private";
        }) allVms
      ));
    in
    {
      systemd.services.status-board = {
        description = "Internal medano fleet dashboard";
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" "libvirtd.service" ];
        path = [ pkgs.libvirt pkgs.coreutils ];
        environment = {
          STATUS_BOARD_INVENTORY = "${vmInventory}";
          STATUS_BOARD_LISTEN = "127.0.0.1:8090";
        };
        serviceConfig = {
          ExecStart = "${package}/bin/status-board";
          Restart = "on-failure";
          RestartSec = 5;
          User = "root";
        };
      };

      services.nginx.virtualHosts."status.medano.emile.space" = {
        enableACME = true;
        forceSSL = true;
        locations."/" = {
          proxyPass = "http://127.0.0.1:8090";
          extraConfig = ''
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_read_timeout 30s;
          '';
        };
      };
    };
in
{
  inherit package module;
}
