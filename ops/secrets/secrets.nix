let
  # users
  # Can't manage these centrally (in //users/<name>/keys), as agenix needs to
  # be executed in this directory and obviously doesn't know about hefe
  hanemile = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPZi43zHEsoWaQomLGaftPE5k0RqVrZyiTtGqZlpWsew emile@caladan";

  # corrino is an admin, so we can deploy from corrino
  corrino = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGHcyg+yw1fwTfpbN1meNluaTXNcl6w1LIL0kz5Kan8I root@corrino";

  admins = [
    hanemile
    corrino
  ];

  # hosts
  medano = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDo2MqY7BD6rd/L3UURx/2kTuHMC7V7WmW74bCsejChq root@medano";
  mail = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGY7/xWbF1+VO6+tGB5IHMr8RHne1A9eEtJvqaPU/dck root@mail";
  lampadas = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAbvlQjEsZO4hsfdUwhVQnxYkxyoRiVxkPGlJO2hzMOl root@lampadas";
  lernaeus = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOASVDM+HusQY7btHM76V0HyllczztxRESaQMnL1PnFi root@lernaeus";

  # vms
  arr = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFCQ5JRwEr8LUmWtMR/Gue/bSiXJxyIk4dKyDi1s2M7o root@arr";
  rou = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID5T9VI4Xj/DcRgU3zBroA0HkFAKvKzdBundjrUNI+as root@rou";
  auth = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID1zv4CBYJEktBzq7FOA5BIeWCSzC5kROnV3dbv1t81L root@auth";
  md = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICK0p/XZiMN8Q64+8lYE1x3I/Q069YLwL4mZAGPwHkCg root@md";
  data = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIABD8kIqLF0oTNKsRDSaK6FYigOrwpUtlePjxwtme+zg root@data";
  photo = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIITS9LszO4+ASdxvys9I5R58+3uwLzb1RwDopLU7JLlI root@photo";
  amalthea = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID5EeR1ZbLxodwNhUKP9hARMZjf5MH3OuTZ+zGToO3ZH root@amalthea";

  # new VM host keys (bootstrapped via image-flow)
  naraj    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJawJxofA1dSc9vuc3Phv4rmRcZ2QYe21AqvmQHYXJYd root@naraj";
  git      = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDpPrG4aH7MRlTlctbEDl9Om7kd71S3s5mPYf8XkPRrf root@git";
  miki     = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOysZe9m8ERyTTtoIRx5IPOPM8VXX3FoN+I4kanzNtdy root@miki";
  social   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDnX+fYOAQSoMxkH6W+JeP81kSdKhsf+BZnBU9XsfCHg root@social";
  rss      = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAB1LLVc5yi7M9Tmf3iYkweNWf6Rhcmz1BihmAqt+n1s root@rss";
  tmp      = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICrSItCIigpmQLpRForEznBHJprcx82BQeeweRmtQ7Pt root@tmp";
  late     = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJVF84a4bUbSEzdUe/6E+3QarQtekMt9Od8QfVU5h9tb root@nixos";
  demo01   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIuR2E3bQYK6nszaVZ5zgIXjnTj3vH56W6tIbFGtEPOS root@demo01";

  # new VMs added 2026-05-24
  sb1       = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKphV2m4+5F2hCMTv121SXQHKxdsnP0O1BNwRCKAfhAe root@sb1";
  sb2       = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAxChO5pLFc2UGDig8cDIFQXf4mA3kUjhKczSK60xFwL root@sb2";
  sb3       = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFwfwmEcMog5xCGBsVSe84y8DBXWVYvRmP8QGh+qkN0p root@sb3";
  minecraft = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA/0Vc34WKHLyJYcZ2KF98PX7A+L5BC0JqLz/5doKDe1 root@minecraft";
  factorio  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBuAa/baIxfJoSCyptxyrhvsINa1XjZxKYmwgReBBUVe root@factorio";
  r2wars    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExKIrQADDElPIh3jARxGrRtEsjUlXcWyhCk/bkT47nC root@r2wars";
  irc       = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIF5pt9Yq/wc/jQOCQ8K7IMB+aGUN7/LSZazWGTLdXcvR root@irc";
  # allHosts = [ medano arr ];

  # convinience function: the hosts themselves and all admins should be able to access the host
  for = hosts: {
    publicKeys = hosts ++ admins;
  };
in
{
  # === medano ===
  "guacamole_user_mapping_xml.age" = for [ medano ];

  # === arr ===
  "mullvad_famous_hog_priv.age" = for [ rou ];

  # === mail ===
  "mail_emile_space_password.age" = for [ mail auth lampadas ];

  # === auth ===
  "authelia_jwt_secret.age" = for [ auth ];
  "authelia_session_secret.age" = for [ auth ];
  "authelia_storage_encryption_key.age" = for [ auth ];
  "authelia_oidc_issuer_private_key.age" = for [ auth ];
  "authelia_oidc_hmac_secret.age" = for [ auth ];
  # oidc client secrets
  "hedgedoc_oidc_client_secret.age" = for [ auth ];
  "sftpgo_oidc_client_secret.age" = for [ auth ];
  "photo_immich_oidc_client_secret.age" = for [ auth ];
  "amalthea_oidc_client_secret.age" = for [ auth ];

  # === md ===
  "hedgedoc_environment_variables.age" = for [ md ];

  # === data ===
  "data.pinto-pike.ts.net.key.age" = for [ data ];
  "data.pinto-pike.ts.net.crt.age" = for [ data ];
  "sftpgo_oidc_client_password.age" = for [ data ];

  # === photo ===
  "photo_immich_secrets_file.age" = for [ photo ];

  # === social ===
  "gotosocial_environment_file.age" = for [ social ];
  "gotosocial_oidc_client_secret.age" = for [ auth social ];

  # === amalthea ===
  "amalthea_oidc_client_password.age" = for [ amalthea ];
  "amalthea_immich_api_key.age" = for [ amalthea ];

  # === lampadas ===
  "immich_secrets_file.age" = for [ lampadas ];
  "lampadas_pinto_pike_ts_net_crt.age" = for [ lampadas ];
  "lampadas_pinto_pike_ts_net_key.age" = for [ lampadas ];
  "lampadas_grafana_admin_password.age" = for [ lampadas ];


  # === rss ===
  "miniflux_admin_creds.age" = for [ rss ];
  "miniflux_oidc_client_secret.age" = for [ rss auth ];


  # === irc ===
  "irc_tls_crt.age" = for [ irc ];
  "irc_tls_key.age" = for [ irc ];

  # === backups ===
  "storagebox_bx11_restic_password.age" = for [ medano mail lampadas auth md photo data naraj git miki social rss tmp amalthea late sb1 sb2 sb3 minecraft factorio r2wars irc ];

  # The config bx11 connection config contains this:
  # username=u331921
  # domain=u331921.your-storagebox.de
  # password=...
  "storagebox_bx11_connection_config.age" = for [ medano mail lampadas auth md photo data naraj git miki social rss tmp amalthea late sb1 sb2 sb3 minecraft factorio r2wars irc ];
}
