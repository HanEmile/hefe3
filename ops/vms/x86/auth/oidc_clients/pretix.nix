{ config, hefe, ... }:

{
  age.secrets = {
    pretix_oidc_client_secret = {
      file = hefe.ops.secrets."pretix_oidc_client_secret.age";
      owner = "authelia-main";
      group = "authelia-main";
    };
  };

  services.authelia.instances.main.settings.identity_providers.oidc.clients = [
    {
      client_id = "pretix";

      # nix run nixpkgs#authelia -- crypto hash generate pbkdf2 --variant sha512 --random --random.length 72 --random.charset rfc3986
      client_secret = "{{ secret \"${config.age.secrets.pretix_oidc_client_secret.path}\" }}";
      public = false;
      authorization_policy = "one_factor";
      redirect_uris = [
        "https://tickets.emile.space/control/auth/oauth/return"
      ];
      scopes = [
        "openid"
        "email"
        "profile"
      ];
      grant_types = [
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
