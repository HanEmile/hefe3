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

        # Forward-auth: every request is gated by authelia. Unauthenticated
        # requests are redirected to the authelia portal, which after a
        # successful login (incl. 2FA per the *.emile.space policy) sets a
        # session cookie scoped to .emile.space and bounces back here.
        locations."/" = {
          proxyPass = "http://127.0.0.1:8090";
          extraConfig = ''
            auth_request /internal/authelia/authz;
            auth_request_set $target_url $scheme://$http_host$request_uri;
            auth_request_set $user $upstream_http_remote_user;
            auth_request_set $groups $upstream_http_remote_groups;
            auth_request_set $name $upstream_http_remote_name;
            auth_request_set $email $upstream_http_remote_email;
            proxy_set_header Remote-User $user;
            proxy_set_header Remote-Groups $groups;
            proxy_set_header Remote-Name $name;
            proxy_set_header Remote-Email $email;
            error_page 401 = @authelia_signin;
            proxy_read_timeout 30s;
          '';
        };

        locations."= /internal/authelia/authz" = {
          # Authelia's forward-auth endpoint returns 200 if authenticated,
          # 302 if the user must be redirected to the login portal. nginx's
          # auth_request only accepts 200 or 401/403 — so we tell authelia
          # to return 401 in the not-authed case via the rd= query string
          # combined with this magic header.
          proxyPass = "http://192.168.75.3:9091/api/authz/forward-auth";
          extraConfig = ''
            internal;
            proxy_set_header X-Forwarded-Method $request_method;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header X-Forwarded-Host $http_host;
            proxy_set_header X-Forwarded-Uri $request_uri;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header Content-Length "";
            proxy_pass_request_body off;
            # Treat any redirect (302) from authelia as 401 so the
            # outer auth_request sees a failure and triggers the
            # @authelia_signin handler.
            proxy_intercept_errors on;
            error_page 301 302 303 307 308 = @authelia_intercept;
          '';
        };

        locations."@authelia_intercept" = {
          extraConfig = ''
            internal;
            return 401;
          '';
        };

        locations."@authelia_signin" = {
          extraConfig = ''
            internal;
            return 302 https://auth.medano.emile.space/?rd=$target_url&rm=$request_method;
          '';
        };
      };
    };
in
{
  inherit package module;
}
