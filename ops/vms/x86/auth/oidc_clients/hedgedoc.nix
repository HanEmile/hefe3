{ config, hefe, ... }:

{
  age.secrets = {
    hedgedoc_oidc_client_secret = {
      file = hefe.ops.secrets."hedgedoc_oidc_client_secret.age";
      owner = "authelia-main";
      group = "authelia-main";
    };
  };

  # auth via authelia
  services.authelia.instances.main.settings.identity_providers.oidc.clients = [
    {
      client_id = "HedgeDoc";

      # ; nix run nixpkgs#authelia -- crypto hash generate pbkdf2 --variant sha512 --random --random.length 72 --random.charset rfc3986
      client_secret = "{{ secret \"${config.age.secrets.hedgedoc_oidc_client_secret.path}\" }}";
      public = false;
      authorization_policy = "two_factor";
      redirect_uris = [ "https://md.medano.emile.space/auth/oauth2/callback" ];
      scopes = [
        "openid"
        "email"
        "profile"
      ];
      grant_types = [
        # "refresh_token"
        "authorization_code"
      ];
      response_types = [ "code" ];
      response_modes = [
        "form_post"
        "query"
        "fragment"
      ];
      token_endpoint_auth_method = "client_secret_post";
    }
  ];
}
