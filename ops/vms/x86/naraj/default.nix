{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.naraj;
in
{
  imports = [
   ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "naraj";
  system.stateVersion = "25.05";

  # naraj is the fleet's public ingress: TLS termination + reverse proxy +
  # ACME. medano DNATs eno1:80,443 to naraj:80,443.
  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ 80 443 6697 ];

  systemd.tmpfiles.rules = [
    "d /var/www/emile.space 0755 nginx nginx - -"
  ];

  security.acme = {
    acceptTerms = true;
    defaults.email = "letsencrypt@emile.space";
  };

  services.nginx = {
    enable = true;
    recommendedGzipSettings = true;
    recommendedOptimisation = true;
    recommendedProxySettings = true;
    recommendedTlsSettings = true;
    recommendedBrotliSettings = true;
    recommendedUwsgiSettings = true;

    virtualHosts =
      let
        tlsify = content: content // {
          forceSSL = true;
          enableACME = true;
        };
      in
      {
        # --- emile.space root (was: served from /var/www/emile.space) ---
        # The wildcard DNS sends everything that doesn't match another vhost
        # here too. Keep the medano.emile.space alias so the old hostname
        # still resolves.
        "emile.space" = tlsify {
          serverAliases = [ "www.emile.space" "medano.emile.space" ];
          locations = {
            "/" = {
              root = "/var/www/emile.space";
              index = "index.html";
              extraConfig = ''
                add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
              '';
            };
            # gotosocial uses redirects from emile.space for @hanemile
            "/@hanemile".extraConfig = ''
              return 301 https://social.emile.space/@hanemile;
            '';
          };
        };

        # --- amalthea (astrophotography) ---
        "amaltheea.medano.emile.space" =
          let
            backend = hefe.ops.ipam.default.amalthea;
          in
          tlsify {
            locations."/" = {
              proxyPass = "http://${backend.v4}:${toString backend.ports.backend}";
              proxyWebsockets = true;
            };
          };

        # --- immich (photo) ---
        "photo.emile.space" =
          let
            backend = hefe.ops.ipam.default.photo;
          in
          tlsify {
            serverAliases = [ "photo.medano.emile.space" ];
            locations."/" = {
              proxyPass = "http://${backend.v4}:${toString backend.ports.immich}";
              proxyWebsockets = true;
              extraConfig = ''
                client_max_body_size 50000M;
                proxy_read_timeout   600s;
                proxy_send_timeout   600s;
                send_timeout         600s;
              '';
            };
          };

        # --- hedgedoc (md) ---
        "md.emile.space" =
          let
            backend = hefe.ops.ipam.default.md;
          in
          tlsify {
            serverAliases = [ "md.medano.emile.space" ];
            locations."/".proxyPass = "http://${backend.v4}:${toString backend.ports.hedgedoc}";
          };

        # --- authelia (single canonical host: sso.emile.space) ---
        # Webauthn rp.id, OIDC issuer, session cookie domain and TOTP issuer
        # are all pinned to sso.emile.space in authelia.nix.
        "sso.emile.space" =
          let
            proxyPass = "http://192.168.75.3:9091";
          in
          tlsify {
            locations = {
              "/".proxyPass = proxyPass;
              "/api/verify".proxyPass = proxyPass;
              "/api/authz/".proxyPass = proxyPass;
            };
          };

        # Legacy hostnames: 301 to canonical so old bookmarks keep working
        # and webauthn rp.id never sees the wrong host.
        "auth.emile.space" = tlsify {
          serverAliases = [ "auth.medano.emile.space" ];
          locations."/".extraConfig = ''
            return 301 https://sso.emile.space$request_uri;
          '';
        };

        # --- gotosocial (social) ---
        "social.emile.space" =
          let
            backend = hefe.ops.ipam.default.social;
          in
          tlsify {
            serverAliases = [ "social.medano.emile.space" ];
            locations."/" = {
              proxyPass = "http://${backend.v4}:3004";
              proxyWebsockets = true;
              extraConfig = ''
                client_max_body_size 40M;
              '';
            };
          };

        # --- tmp.emile.space (static autoindex) ---
        "tmp.emile.space" = tlsify {
          serverAliases = [ "tmp.medano.emile.space" ];
          locations."/" = {
            proxyPass = "http://${hefe.ops.ipam.default.tmp.v4}";
            extraConfig = ''
              add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
            '';
          };
        };

        # --- status-board (medano-side service, behind authelia forward-auth) ---
        "status.emile.space" = tlsify {
          serverAliases = [ "status.medano.emile.space" ];

          locations."/" = {
            # medano host on virbr0 listens on :8090 for the status-board.
            proxyPass = "http://192.168.75.1:8090";
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
              return 302 https://sso.emile.space/?rd=$target_url&rm=$request_method;
            '';
          };
        };

        # --- irc.emile.space: ACME http-01 only ---
        # IRC is not HTTP, so this vhost exists solely to obtain/renew the
        # Let's Encrypt cert for irc.emile.space. The actual IRC traffic is
        # handled by the nginx stream block below (TLS-terminating TCP proxy
        # to Ergo on the irc VM). Port 80 ACME challenges land here; 443 just
        # returns a small notice.
        "irc.emile.space" = tlsify {
          locations."/".extraConfig = ''
            default_type text/plain;
            return 200 "irc.emile.space is an IRC server. Connect with an IRC client on port 6697 (TLS).";
          '';
        };
      };
  };

  # ---------------------------------------------------------------------------
  # IRC ingress: TLS-terminating TCP stream proxy.
  # nginx terminates TLS for irc.emile.space using the ACME cert above, then
  # forwards plaintext IRC to Ergo on the irc VM. medano DNATs :6697 -> naraj.
  # ---------------------------------------------------------------------------
  services.nginx.streamConfig = ''
    upstream ergo_backend {
      server ${hefe.ops.ipam.default.irc.v4}:6667;
    }
    server {
      listen 6697 ssl;
      ssl_certificate     /var/lib/acme/irc.emile.space/fullchain.pem;
      ssl_certificate_key /var/lib/acme/irc.emile.space/key.pem;
      proxy_pass ergo_backend;
      proxy_timeout 1h;
    }
  '';

  # First-boot bootstrap: if ACME has not issued the irc.emile.space cert yet,
  # drop a self-signed placeholder at the expected path so the nginx stream
  # block can start. ACME replaces it on its first successful run, after which
  # the postRun hook below reloads nginx to pick up the real cert.
  systemd.services.nginx = {
    wants = [ "acme-irc.emile.space.service" ];
    after = [ "acme-irc.emile.space.service" ];
  };

  systemd.services.irc-stream-cert-bootstrap = {
    description = "Seed a self-signed placeholder cert for the irc.emile.space stream";
    wantedBy = [ "nginx.service" ];
    before = [ "nginx.service" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    script = ''
      set -eu
      D=/var/lib/acme/irc.emile.space
      if [ ! -f "$D/fullchain.pem" ] || [ ! -f "$D/key.pem" ]; then
        mkdir -p "$D"
        ${pkgs.openssl}/bin/openssl req -x509 -newkey rsa:2048 -nodes \
          -keyout "$D/key.pem" -out "$D/fullchain.pem" \
          -subj "/CN=irc.emile.space" -days 3650
        # Match ACME's ownership so its renewal can overwrite these later.
        chown acme:nginx "$D" "$D/fullchain.pem" "$D/key.pem" || true
        chmod 0640 "$D/fullchain.pem" "$D/key.pem" || true
      fi
    '';
  };

  # Reload nginx after ACME (re)issues the irc cert so the stream picks it up.
  security.acme.certs."irc.emile.space".postRun = ''
    systemctl reload nginx.service || true
  '';

  # external probes from naraj's own viewpoint
  services.healthProbes.probes = [
    { name = "self-root"; url = "http://${ipam.v4}/"; }
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}
