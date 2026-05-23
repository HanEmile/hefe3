# readTree options
{
  hefe,
  lib,
  pkgs,
  ...
}@args1:

# passed by module system
{
  config,
  nixvirt,
  ...
}@args2:

let
  mod = name: hefe.path.origSrc + ("/ops/modules/" + name);
  vm = name: hefe.path.origSrc + ("/ops/vms/x86/" + name + "/libvirt.nix");
in
{
  imports = [
    (import ./boot.nix (args1 // args2))
    ./networking.nix
    ./hardware-configuration.nix
    ./libvirt

    hefe.tools.sshrouter.module

    (mod "ports.nix")
    ../../../vms/x86/modules/healthProbes.nix
    (hefe.tools.status-board.module { inherit hefe; })

    (vm "naraj") # general nginx router
    (vm "rou") # route VPN traffic
    (vm "arr") # media
    (vm "auth") # sso
    (vm "md") # hedgedoc
    (vm "git") # git
    (vm "data") # data
    (vm "miki") # md wiki
    (vm "photo") # public immich
    (vm "rss") # rss feed
    (vm "social") # gotosocial
    (vm "tmp") # tmpfile host
    (vm "amalthea") # astrophotography
    (vm "late") # community
    (vm "demo01") # image-bootstrap demo VM
    (vm "sb1") # standby linux vm
    (vm "sb2") # standby linux vm
    (vm "sb3") # standby linux vm

    # ctf

    # syzkaller fuzzing VM pool
    # (import (hefe.path.origSrc + "/ops/vms/x86/fuzz/libvirt.nix") {
    #   inherit nixvirt;
    #   pool = [
    #     # Example: uncomment and set kernel paths to activate fuzz VMs
    #     # {
    #     #   name = "fuzz-mainline";
    #     #   kernel = /keep/kernel/bzImage-mainline;
    #     # }
    #     # {
    #     #   name = "fuzz-patched";
    #     #   kernel = /keep/kernel/bzImage-patched;
    #     #   initrd = /keep/kernel/initrd-patched.img;
    #     #   memory = 4;
    #     #   vcpu_count = 4;
    #     #   extra_cmdline = "ksan.fault=panic";
    #     # }
    #   ];
    # })
  ];

  age.secrets = {
    storagebox_bx11_restic_password = {
      file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
    };
    storagebox_bx11_connection_config = {
      file = hefe.ops.secrets."storagebox_bx11_connection_config.age";
    };
  };

  fileSystems = {
    "/proc" = {
      device = "/proc";
      options = [
        "nosuid"
        "nodev"
        "noexec"
        "relatime" # normal foo

        # mount -o remount,hidepid=2 /proc
        "hidepid=2" # this makes sure users can only see their own processes
      ];
    };

    "/mnt/storagebox-bx11" = {
      device = "//u331921.your-storagebox.de/backup";
      fsType = "cifs";
      options =
        let
          conn_config = config.age.secrets."storagebox_bx11_connection_config".path;
        in
        [
          "_netdev,x-systemd.automount,noauto,x-systemd.idle-timeout=60s,x-systemd.device-timeout=5s,x-systemd.mount-timeout=5s,credentials=${conn_config}"
        ];
    };
  };

  security.acme = {
    acceptTerms = true;
    defaults.email = "letsencrypt@emile.space";
  };

  users =
    let
      aclconf = with hefe.ops.acl; (usersForHost host."${config.networking.hostName}");
    in
    {
      users = aclconf.users // {
        # Just manually adding this here as a sort of "safeguard", I don't want
        # to accidentally remove the key from the ACL and be stuck without a con
        root.openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;
      };
      groups = aclconf.groups;
    };

  environment.systemPackages = with pkgs; [
    vim
    libgbm
    cifs-utils

    # for the networkd-dispatcher service used by tailscale
    ethtool
    networkd-dispatcher
  ];

  hardware.enableRedistributableFirmware = true;

  nix.settings.trusted-users = [
    "root"
    "@wheel"
  ];

  programs = {
    mosh.enable = true;
  };

  services = {
    openssh = {
      enable = true;
      ports = [
        22
        2222
      ];
      settings.PasswordAuthentication = false;
    };

    tailscale = {
      enable = true;
      extraUpFlags = [ "--ssh --advertise-exit-node" ];
      # interfaceName = "enp0s31f6";
    };

    networkd-dispatcher = {
      enable = true;
      rules."50-tailscale" = {
        onState = [ "routable" ];
        script = ''
          "${pkgs.ethtool}/sbin/ethtool" -K "${config.services.tailscale.interfaceName}" rx-udp-gro-forwarding on rx-gro-list off
        '';
      };
    };

    vnstat.enable = true;

    nfs.server = {
      enable = true;
      exports = ''
        /grave/data  192.168.75.7/32(rw,async,no_root_squash,no_subtree_check)
        /grave/media 192.168.33.3/32(rw,async,no_root_squash,no_subtree_check)
      '';
      lockdPort = 4001;
      mountdPort = 4002;
      statdPort = 4000;
      extraNfsdConfig = "";
    };

    # SSH Router - routes users to target hosts
    sshrouter = {
      enable = false;
      listenHost = "0.0.0.0";
      listenPort = 22;
      routes = {
        # Add user -> target mappings here
        hanemile = "${hefe.ops.ipam.default.miki.v4}:22";
        root-arr = "${hefe.ops.ipam.private.arr.v4}:22";
      };
      default = "${hefe.ops.ipam.default.naraj.v4}:22";
    };

    # TODO: figure out what zfs datasets to backup
    # sanoid = {
    #   enable = true;
    # };

    nginx = {
      enable = true;
      enableReload = true;

      recommendedUwsgiSettings = true;
      recommendedTlsSettings = true;
      recommendedProxySettings = true;
      recommendedOptimisation = true;
      recommendedGzipSettings = true;
      recommendedBrotliSettings = true;

      virtualHosts =
        let
          tlsify =
            content:
            content
            // {
              forceSSL = true;
              enableACME = true;
            };
        in
        {
          "amaltheea.medano.emile.space" =
            let
              amalthea = hefe.ops.ipam.default.amalthea;
              host = amalthea.v4;
              port = amalthea.ports.backend;
            in
            tlsify {
               locations."/" = {
                 proxyPass = "http://${host}:${toString port}";
                 proxyWebsockets = true;
               };
            };

          "photo.medano.emile.space" =
            let
              host = hefe.ops.ipam.default.photo.v4;
              port = hefe.ops.ipam.default.photo.ports.immich;
            in
            tlsify {
              locations."/" = {
                proxyPass = "http://${host}:${toString port}";
                proxyWebsockets = true;
                extraConfig = ''
                  client_max_body_size 50000M;
                  proxy_read_timeout   600s;
                  proxy_send_timeout   600s;
                  send_timeout         600s;
                '';
              };
            };

          "md.medano.emile.space" =
            let
              host = hefe.ops.ipam.default.md.v4;
              port = hefe.ops.ipam.default.md.ports.hedgedoc;
            in
            tlsify {
              locations."/".proxyPass = "http://${host}:${toString port}";
            };

          "auth.medano.emile.space" =
            let
              proxyPass = "http://192.168.75.3:9091";
            in
            tlsify {
              locations = {
                "/" = {
                  inherit proxyPass;

                  extraConfig = ''
                    ## Headers
                    # proxy_set_header Host $host;
                    # proxy_set_header X-Original-URL $scheme://$http_host$request_uri;
                    # proxy_set_header X-Forwarded-Proto $scheme;
                    # proxy_set_header X-Forwarded-Host $http_host;
                    # proxy_set_header X-Forwarded-URI $request_uri;
                    # proxy_set_header X-Forwarded-Ssl on;
                    # proxy_set_header X-Forwarded-For $remote_addr;
                    # proxy_set_header X-Real-IP $remote_addr;

                    ## Basic Proxy Configuration
                    # client_body_buffer_size 128k;
                    # proxy_next_upstream error timeout invalid_header http_500 http_502 http_503; ## Timeout if the real server is dead.
                    # proxy_redirect  http://  $scheme://;
                    # proxy_http_version 1.1;
                    # proxy_cache_bypass $cookie_session;
                    # proxy_no_cache $cookie_session;
                    # proxy_buffers 64 256k;

                    ## Trusted Proxies Configuration
                    ## Please read the following documentation before configuring this:
                    ##     https://www.authelia.com/integration/proxies/nginx/#trusted-proxies
                    # set_real_ip_from 10.0.0.0/8;
                    # set_real_ip_from 172.16.0.0/12;
                    # set_real_ip_from 192.168.0.0/16;
                    # set_real_ip_from fc00::/7;
                    # set_real_ip_from 127.0.0.1/32;
                    # real_ip_header X-Forwarded-For;
                    # real_ip_recursive on;

                    ## Advanced Proxy Configuration
                    # send_timeout 5m;
                    # proxy_read_timeout 360;
                    # proxy_send_timeout 360;
                    # proxy_connect_timeout 360;
                  '';
                };

                "/api/verify" = {
                  inherit proxyPass;
                };

                "/api/authz/" = {
                  inherit proxyPass;
                };
              };
            };

          "medano.emile.space" = tlsify {
            locations = {
              "/" = {
                proxyPass = "http://${hefe.ops.ipam.default.naraj.v4}";
                extraConfig = ''
                  add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
                '';
              };

              # "/info/" = {
              #   extraConfig =
              #     let
              #       acl = hefe.ops.acl;
              #       hosts = builtins.attrNames acl.host;
              #       usersForHost = x: acl.usersForHost acl.host."${x}";
              #       a = builtins.toJSON (map usersForHost hosts);
              #       filePath = pkgs.writeText "usersforhost.json" a;
              #     in
              #     ''
              #       add_header Content-Type application/json;
              #       alias ${filePath};
              #     '';
              # };

              "/guacamole" = {
                proxyPass = "http://127.0.0.1:8080";
                extraConfig = ''
                  proxy_buffering off;
                  proxy_http_version 1.1;
                  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
                  proxy_set_header Upgrade $http_upgrade;
                  proxy_set_header Connection $http_connection;
                  access_log off;
                '';
              };

              # As the social.emile.space server actually uses redirects from emile.space, they have to be
              # setup somewhere. Well... this is that place
              "/@hanemile".extraConfig = ''
                return 301 https://social.emile.space/@hanemile;
              '';

              #"/.well-known" = {
              #  root = "/var/www/emile.space";
              #  extraConfig = ''
              #    autoindex on;
              #  '';
              #};

              ## I ran a matrix homeserver for some time, then stopped, but the other
              ## homeserver don't know and don't stop sending me requests (5e5 a day or
              ## so).
              #"/.well-known/matrix/server".extraConfig = ''
              #  return 410;
              #'';
            };
          };

          "tmp.medano.emile.space" = tlsify {
            locations = {
              "/" = {
                proxyPass = "http://${hefe.ops.ipam.default.tmp.v4}";
                extraConfig = ''
                  add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
                '';
              };
            };
          };
        };
    };
  };

  system.stateVersion = "25.05";

  # External health probes for every public service medano fronts.
  services.healthProbes.probes = [
    { name = "medano-root";      url = "https://medano.emile.space/"; }
    { name = "auth";             url = "https://auth.medano.emile.space/api/health"; }
    { name = "md";               url = "https://md.medano.emile.space/status"; }
    { name = "photo";            url = "https://photo.medano.emile.space/api/server/ping"; }
    { name = "amalthea";         url = "https://amaltheea.medano.emile.space/"; expectedStatus = 502; }
    { name = "tmp";              url = "https://tmp.medano.emile.space/"; }
  ];

  # Prometheus node-exporter for medano itself. Bind on tailscale + localhost
  # only so the host's :9100 is not publicly reachable.
  services.prometheus.exporters.node = {
    enable = true;
    listenAddress = "0.0.0.0";
    port = 9100;
    enabledCollectors = [ "systemd" "logind" "processes" ];
  };

  networking.firewall.interfaces."tailscale0".allowedTCPPorts = [ 9100 ];
}
