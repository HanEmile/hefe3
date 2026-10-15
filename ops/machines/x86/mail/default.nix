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
    (import ../../../modules/storagebox.nix { inherit hefe; })
  ];

  age = {
    secrets = {
      storagebox_bx11_restic_password = {
        file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
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
    # 80: ACME http-01 challenges (the mailserver 26.05 module no longer
    # opens this itself; 25.11 did when its acme-nginx scheme was in use).
    firewall.allowedTCPPorts = [ 80 ];

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

    # nginx was implicitly enabled by the mailserver module's (now removed)
    # acme-nginx scheme; enable it explicitly as it serves the ACME
    # challenges and the prometheus proxy.
    nginx.enable = true;

    nginx.virtualHosts = {
      # Answers ACME http-01 challenges for the mail server's own cert.
      # The nixos-mailserver 26.05 module no longer ships a built-in
      # acme-nginx scheme; per its setup guide the main nginx does this now.
      # acmeRoot is required: with the default (null) the cert's webroot
      # stays null and the acme module refuses to build.
      "mail.emile.space" = {
        enableACME = true;
        acmeRoot = "/var/lib/acme/acme-challenge";
      };

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

  # Same mount-namespace gotcha as lampadas (see its default.nix): the
  # restic unit runs with a private mount namespace that never sees the
  # storagebox autofs mount, so restic's pre-start failed with
  # "mkdir /mnt/storagebox-bx11: file exists". RequiresMountsFor activates
  # the automount and propagates it into the unit's namespace.
  systemd.services."restic-backups-mail".unitConfig.RequiresMountsFor = "/mnt/storagebox-bx11";

  system.stateVersion = "21.11";
}
