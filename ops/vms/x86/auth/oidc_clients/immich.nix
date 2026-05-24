{ config, hefe, ... }:

# this is the oidc config for the immich on the photos vm

{
  age.secrets = {
    photo_immich_oidc_client_secret = {
      file = hefe.ops.secrets."photo_immich_oidc_client_secret.age";
      owner = "authelia-main";
      group = "authelia-main";
    };
  };

  # auth via authelia
  services.authelia.instances.main.settings.identity_providers.oidc.clients = [
    {
      client_id = "Immich";

      # ; nix run nixpkgs#authelia -- crypto hash generate pbkdf2 --variant sha512 --random --random.length 72 --random.charset rfc3986
      client_secret = "{{ secret \"${config.age.secrets.photo_immich_oidc_client_secret.path}\" }}";
      public = false;
      authorization_policy = "one_factor";
      redirect_uris = [
        "https://photo.medano.emile.space/auth/login"
        "https://photo.medano.emile.space/user-settings"
        "app.immich:///oauth-callback"
      ];
      scopes = [
        "openid"
        "email"
        "profile"
      ];
      token_endpoint_auth_method = "client_secret_post";
    }
  ];
}
