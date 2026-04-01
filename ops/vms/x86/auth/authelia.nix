{
  pkgs,
  hefe,
  config,
  lib,
  ...
}@args:

{
  imports = [
    # TODO(emile): convert this to a module
    # https://discourse.nixos.org/t/passing-parameters-into-import/34082/4
    (import ./oidc_clients/hedgedoc.nix args)
    (import ./oidc_clients/sftpgo.nix args)
  ];

  users.users."authelia-main" = {
    name = "authelia-main";
  };

  users.groups."authelia-main" = {
    name = "authelia-main";
    members = [ "authelia-main" ];
  };

  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ 9091 ];

  # Export TOTP using
  # ; /nix/store/mvyzaw49dry71b2qa2hj61a3hy1s7acc-authelia-4.39.10/bin/authelia --config /nix/store/vd5al67gxga9csvvggysi7ac68jqg5pb-config.yml storage user totp export uri --encryption-key $(cat /run/agenix/authelia_storage_encryption_key)

  # set the permissions for the secrets...
  age.secrets =
    let
      owner = "authelia-main";
      group = "authelia-main";

      # add the owner and group to all attributes
      withOwnerGroup = fileSet: lib.mapAttrs (_: value: value // { inherit owner group; }) fileSet;

    in
    withOwnerGroup {
      # ... passwed via environment vars
      authelia_session_secret = {
        file = hefe.ops.secrets."authelia_session_secret.age";
        inherit owner group;
      };
      mail_emile_space_password = {
        file = hefe.ops.secrets."mail_emile_space_password.age";
        inherit owner group;
      };

      # ... passed via the services.authelia.instances.main.secrets attribute
      authelia_storage_encryption_key = {
        file = hefe.ops.secrets."authelia_storage_encryption_key.age";
        inherit owner group;
      };
      authelia_jwt_secret = {
        file = hefe.ops.secrets."authelia_jwt_secret.age";
        inherit owner group;
      };
      authelia_oidc_issuer_private_key = {
        file = hefe.ops.secrets."authelia_oidc_issuer_private_key.age";
        inherit owner group;
      };
      authelia_oidc_hmac_secret = {
        file = hefe.ops.secrets."authelia_oidc_hmac_secret.age";
        inherit owner group;
      };
    };

  services.authelia.instances = {
    main = {
      enable = true;
      package = pkgs.authelia;

      # pass some of the secrets in as env-vars
      environmentVariables = with config.age.secrets; {
        AUTHELIA_SESSION_SECRET_FILE = authelia_session_secret.path;
        AUTHELIA_NOTIFIER_SMTP_PASSWORD_FILE = mail_emile_space_password.path;
      };
      secrets = with config.age.secrets; {
        manual = true;

        # some other secrets can be defined here, but not all...
        storageEncryptionKeyFile = authelia_storage_encryption_key.path;
        jwtSecretFile = authelia_jwt_secret.path;
        oidcIssuerPrivateKeyFile = authelia_oidc_issuer_private_key.path;
        oidcHmacSecretFile = authelia_oidc_hmac_secret.path;
      };
      settings = {
        theme = "dark";

        server =
          let
            host = hefe.ops.ipam.default.auth.v4;
            port = hefe.ops.ipam.default.auth.ports.authelia;
          in
          {
            address = "${host}:${toString port}";
            # address = "127.0.0.1:${toString config.local.ports.authelia}";
            # host = "127.0.0.1";
            # port = config.emile.ports.authelia;
          };

        # we're using a file to store the user information
        authentication_backend = {
          refresh_interval = "60s";
          file = {
            path = "/var/lib/authelia-main/user.yml";
            watch = true;
            password = {
              algorithm = "argon2id";
              iterations = 3;
              key_length = 32;
              salt_length = 16;
              memory = 65;
              parallelism = 4;
            };
          };
        };

        storage.local.path = "/var/lib/authelia-main/db.sqlite";

        session = {
          # domain = "sso.emile.space";
          # expiration = 3600; # 1 hour
          # inactivity = 300; # 5 minutes

          cookies = [
            {
              domain = "emile.space";
              authelia_url = "https://auth.medano.emile.space";
              # The period of time the user can be inactive for until the session is destroyed. Useful if you want long session timers but don’t want unused devices to be vulnerable.
              inactivity = "1h";
              # The period of time before the cookie expires and the session is destroyed. This is overridden by remember_me when the remember me box is checked.
              expiration = "1d";
              # The period of time before the cookie expires and the session is destroyed when the remember me box is checked. Setting this to -1 disables this feature entirely for this session cookie domain
              remember_me = "3M";
            }
          ];
        };

        notifier = {
          disable_startup_check = false;
          smtp = {
            address = "smtp://mail.emile.space:587";
            # host = "mail.emile.space";
            # port = 587;

            timeout = "30s";
            username = "mail@emile.space";
            # password set in AUTHELIA_NOTIFIER_SMTP_PASSWORD_FILE env var

            sender = "mail@emile.space";
            subject = "[Authelia] {title}";

            disable_require_tls = false;
            disable_starttls = false;
            disable_html_emails = true;

            tls = {
              server_name = "mail.emile.space";
              skip_verify = true;
              minimum_version = "TLS1.3";
            };
          };
        };

        identity_providers = {
          oidc = {
            # regenerate keys like this:
            # ; nix run nixpkgs#authelia -- crypto certificate rsa generate
            # current serial: deb83f17e27e663f544a16ad2947631d

            enable_client_debug_messages = false;
            minimum_parameter_entropy = 8;
            enforce_pkce = "public_clients_only";
            enable_pkce_plain_challenge = false;
            cors = {
              endpoints = [
                "authorization"
                "token"
                "revocation"
                "introspection"
              ];
              allowed_origins = [ "https://emile.space" ];
              allowed_origins_from_client_redirect_uris = false;
            };
          };
        };

        access_control = {
          default_policy = "deny";
          rules = [
            {
              # silverbullet needs access to these without auth
              domain = "sb.emile.space";
              policy = "bypass";
              resources = [
                "/.client/manifest.json$"
                "/.client/[a-zA-Z0-9_-]+.png$"
                "/service_worker.js$"
              ];
            }
            {
              domain = "*.emile.space";
              policy = "two_factor";
            }
          ];
        };

        # type = types.enum [ "" "totp" "webauthn" "mobile_push" ];
        default_2fa_method = "totp";
        totp = {
          disable = false;
          issuer = "auth.medano.emile.space";
          algorithm = "sha1";
          digits = 6;
          period = 30;
          skew = 1;
          secret_size = 32;
        };

        ntp = {
          address = "time.cloudflare.com:123";
          version = 3;
          max_desync = "3s";
          disable_startup_check = false;
          disable_failure = false;
        };
      };
    };
  };
}
