{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

let
  tailscale_address = "100.104.120.80";
in
{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } {inherit hefe pkgs;})
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  environment.systemPackages = with pkgs; [ nfs-utils ];

  networking.hostName = "data";

  fileSystems."/data" = {
    device = "192.168.75.1:/grave/data";
    fsType = "nfs";
    options = [
      "nolock"
      "_netdev"
      "nconnect=16"
    ];
  };

  system.stateVersion = "25.05";

  security.acme.defaults.email = "admin@emile.space";

  age = {
    secrets = {
      data_pinto_pike_ts_net_crt = {
        file = hefe.ops.secrets."data.pinto-pike.ts.net.crt.age";
        owner = "nginx";
        group = "nginx";
      };
      data_pinto_pike_ts_net_key = {
        file = hefe.ops.secrets."data.pinto-pike.ts.net.key.age";
        owner = "nginx";
        group = "nginx";
      };
      "sftpgo_oidc_client_password" = {
        file = hefe.ops.secrets."sftpgo_oidc_client_password.age";
        owner = "sftpgo";
        group = "sftpgo";
      };
    };
    identityPaths = [
      "/etc/ssh/ssh_host_ed25519_key"
      "/etc/ssh/ssh_host_rsa_key"
    ];
  };

  # sftpgo binds on the tailscale IP, wait for the interface to come up.
  systemd.services.sftpgo = {
    after = [ "tailscaled.service" "network-online.target" ];
    wants = [ "tailscaled.service" "network-online.target" ];
  };

  services = {
    openssh = {
      enable = true;
      listenAddresses = [
        # http only via the internal ip on the default bridge
        # otherwise this binds on the tailscale interface as well and blocks
        # the port for sftp
        (with hefe.ops.ipam.default.data; {
          addr = v4;
          port = 22;
        })
      ];
    };
    tailscale.enable = true;

    nginx = {
      enable = true;
      enableReload = true;

      recommendedUwsgiSettings = true;
      recommendedTlsSettings = true;
      recommendedProxySettings = true;
      recommendedOptimisation = true;
      recommendedGzipSettings = true;
      recommendedBrotliSettings = true;

      virtualHosts."data.pinto-pike.ts.net" = {
        serverName = "data.pinto-pike.ts.net";
        listenAddresses = [ tailscale_address ];

        forceSSL = true;
        sslCertificate = config.age.secrets."data_pinto_pike_ts_net_crt".path;
        sslCertificateKey = config.age.secrets."data_pinto_pike_ts_net_key".path;

        locations."/" = {
          proxyPass = with hefe.ops.ipam.default.data; "http://${v4}:${toString ports.sftpgo.web}";
        };
      };
    };

    sftpgo = {
      enable = true;
      settings = {

        # sftp only via tailscale
        sftpd.bindings = [
          (with hefe.ops.ipam.tailscale.data; {
            address = v4;
            port = ports.sftpgo.sftp;
          })
        ];

        # http only via the reverse proxy on medano
        httpd.bindings = [
          (with hefe.ops.ipam.default.data; {
            address = v4;
            port = ports.sftpgo.web;
            proxy_allowed = [ "127.0.0.1" ];
          })
        ];

        oidc = {
          client_id = "sftpgo";
          client_secret_file = config.age.secrets."sftpgo_oidc_client_password".path;
          config_url = "https://sso.emile.space";
          redirect_base_url = "data.medano.emile.space";
          scopes = [
            "openid"
            "email"
            "profile"
          ];
        };
      };
    };
  };

  # Backup sftpgo state. The user data is on the NFS /grave/data dataset
  # (backed up by medano's restic).
  vmBackups.paths = [
    "/var/lib/sftpgo"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${hefe.ops.ipam.default.data.v4}:${toString hefe.ops.ipam.default.data.ports.sftpgo.web}/healthz"; }
  ];
}
