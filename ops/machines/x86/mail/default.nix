# readTree options
{
  hefe,
  pkgs,
  ...
}:
# passed by module system
{ config, ... }:

{
  imports = [
    ./hardware-configuration.nix
    ./mail.nix
  ];

  age = {
    secrets = {
      mail_emile_space_password = {
        file = hefe.ops.secrets."mail_emile_space_password.age";
      };
      storagebox_bx11_restic_password = {
        file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
      };
      storagebox_bx11_connection_config = {
        file = hefe.ops.secrets."storagebox_bx11_connection_config.age";
      };
    };
  };


  boot.loader.grub.enable = true;
  boot.loader.grub.devices = [ "/dev/sda" ];
  boot.kernelPackages = pkgs.linuxPackages_latest;

  networking = {
    hostName = "mail";
    domain = "emile.space";

    firewall.enable = true;

    useDHCP = false;
    interfaces.ens3.useDHCP = true;
  };

  environment.systemPackages = with pkgs; [
    vim
    htop
    ripgrep
    fd
  ];

  services = {
    openssh.enable = true;
    openssh.settings.PermitRootLogin = "prohibit-password";

    nginx.virtualHosts = {
      "prometheus.mail.emile.space" = {
        addSSL = true;
        enableACME = true;
        locations."/" = {
          proxyPass = "http://${config.services.prometheus.listenAddress}:${toString config.services.prometheus.port}/";
          proxyWebsockets = true;
        };
      };
    };

    prometheus = {
      enable = true;
      retentionTime = "356d";

      # prometheus listens on the loopback, the exporters on [::]
      listenAddress = "[::1]";
      port = 9003;

      exporters = {
        node = {
          enable = true;
          enabledCollectors = [ "systemd" ];
          port = 9002;
          openFirewall = true;
        };
        systemd = {
          enable = true;
          port = 9558;
          openFirewall = true;
        };
        smartctl = {
          enable = true;
          port = 9633;
          openFirewall = true;
        };
        nginx = {
          enable = true;
          port = 9913;
          openFirewall = true;
        };


        restic =
          let
            interface = "ens3";
          in
          rec {
            enable = true;
            port = 9753;

            repository = "/mnt/storagebox-bx11/backup/mail";

            # passwordFile = "/etc/nixos/keys/restic_password";
            passwordFile = config.age.secrets."storagebox_bx11_restic_password".path;

            openFirewall = true;

            firewallRules = ''
              iifname "${interface}" tcp dport ${toString port} counter accept
            '';

            firewallFilter = "-i ${interface} -p tcp -m tcp --dport ${toString port}";
          };
      };

      # Give it a name and it creates a job with that name and get's the
      # exporter from prometheus with that name and it's port
      scrapeConfigs = let
        staticTarget = name: {
          job_name = name;
          static_configs = [
            {
              targets = [
                "localhost:${toString config.services.prometheus.exporters.${name}.port}"
              ];
            }
          ];
        };
      in [
        (staticTarget "node")
        (staticTarget "systemd")
        (staticTarget "smartctl")
        (staticTarget "restic")
      ];

    };

    restic.backups."mail" = {
      # user = "u331921";
      timerConfig = {
        OnCalendar = "daily";
        Persistent = true;
      };
      repository = "/mnt/storagebox-bx11/backup/mail";
      initialize = true;
      # passwordFile = "/etc/nixos/keys/restic_password";
      passwordFile = config.age.secrets."storagebox_bx11_restic_password".path;

      paths = [ "/var/vmail" ];
      pruneOpts = [
        "--keep-daily 7"
        "--keep-weekly 5"
        "--keep-monthly 12"
        "--keep-yearly 75"
      ];
    };
  };

  fileSystems."/mnt/storagebox-bx11" = {
    device = "//u331921.your-storagebox.de/backup";
    fsType = "cifs";
    options = let
      conn_config = config.age.secrets."storagebox_bx11_connection_config".path;
    in [
      "_netdev,x-systemd.automount,noauto,x-systemd.idle-timeout=60s,x-systemd.device-timeout=5s,x-systemd.mount-timeout=5s,credentials=${conn_config}"
    ];
  };

  users.users.root = {
    initialHashedPassword = "";
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPZi43zHEsoWaQomLGaftPE5k0RqVrZyiTtGqZlpWsew emile@caladan"
    ];
  };

  security.acme = {
    acceptTerms = true;
    defaults.email = "security@emile.space";
    certs."mail.emile.space".email = "security@emile.space";
  };

  system.stateVersion = "21.11";
}
