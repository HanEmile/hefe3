{ config, lib, pkgs, ... }:

let
  cfg = config.services.sshub;
  sshub = import ./default.nix { inherit pkgs; };
in
{
  options.services.sshub = {
    enable = lib.mkEnableOption "sshub SSH community hub";

    listen = lib.mkOption {
      type = lib.types.str;
      default = "0.0.0.0:2222";
      description = "Address and port to listen on.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/sshub";
      description = "Directory for the SQLite database.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "sshub";
      description = "System user to run sshub as.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "sshub";
      description = "System group to run sshub as.";
    };

    package = lib.mkOption {
      type = lib.types.package;
      default = sshub;
      defaultText = lib.literalExpression "pkgs.callPackage ./default.nix { }";
      description = "The sshub package to use.";
    };

    mosh = {
      enable = lib.mkEnableOption "mosh transport for sshub" // { default = true; };

      portRange = lib.mkOption {
        type = lib.types.attrsOf lib.types.int;
        default = { from = 60000; to = 61000; };
        description = ''
          UDP port range that mosh-server picks ephemeral ports from.
          Matches mosh's default range.
        '';
      };

      openFirewall = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Whether to open the UDP port range in the firewall.";
      };
    };

    authorizedKeys = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = lib.literalExpression ''
        [
          "ssh-ed25519 AAAAC3... alice@laptop"
          "ssh-ed25519 AAAAC3... bob@desktop"
        ]
      '';
      description = ''
        List of SSH public keys allowed to log in. When non-empty, only
        these keys may authenticate. When empty and `authorizedKeysFile`
        is also null, anyone with any key may log in.
      '';
    };

    authorizedKeysFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      description = ''
        Path to an existing authorized_keys file. Useful when keys come
        from another source (e.g. a sops-decrypted secret). If both this
        and `authorizedKeys` are set, the two are merged.
      '';
    };
  };

  config = lib.mkIf cfg.enable (
    let
      # Combine inline keys and the optional file into a single file we hand
      # to sshub via env. An empty resulting list means "no restriction".
      inlineKeysFile =
        if cfg.authorizedKeys != [ ]
        then pkgs.writeText "sshub-authorized-keys-inline" (lib.concatStringsSep "\n" cfg.authorizedKeys + "\n")
        else null;

      authorizedFile =
        if cfg.authorizedKeys != [ ] && cfg.authorizedKeysFile != null then
          pkgs.runCommand "sshub-authorized-keys" { } ''
            cat ${inlineKeysFile} ${cfg.authorizedKeysFile} > $out
          ''
        else if cfg.authorizedKeys != [ ] then inlineKeysFile
        else cfg.authorizedKeysFile;
    in {
      users.users.${cfg.user} = {
        isSystemUser = true;
        group = cfg.group;
        home = cfg.dataDir;
        createHome = true;
      };

      users.groups.${cfg.group} = { };

      systemd.services.sshub = {
        description = "sshub SSH community hub";
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" ];

        # mosh-server reads /etc/locale.conf-style locale settings; UTF-8 is required.
        environment = {
          SSHUB_LISTEN = cfg.listen;
          SSHUB_DB = "${cfg.dataDir}/sshub.db";
          LANG = "C.UTF-8";
          LC_ALL = "C.UTF-8";
        } // lib.optionalAttrs (authorizedFile != null) {
          SSHUB_AUTHORIZED_KEYS = "${authorizedFile}";
        };

        # mosh-server needs to be on PATH; the package wrapper handles this,
        # but bake in `mosh` as a path dep for clarity / future maintenance.
        path = lib.optional cfg.mosh.enable pkgs.mosh;

        serviceConfig = {
          ExecStart = "${cfg.package}/bin/sshub";
          User = cfg.user;
          Group = cfg.group;
          StateDirectory = "sshub";
          Restart = "on-failure";
        };
      };

      networking.firewall = lib.mkIf (cfg.mosh.enable && cfg.mosh.openFirewall) {
        allowedUDPPortRanges = [ cfg.mosh.portRange ];
      };
    }
  );
}
