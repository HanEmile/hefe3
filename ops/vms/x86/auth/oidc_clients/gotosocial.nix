{ config, hefe, ... }:

{
  age.secrets = {
    gotosocial_oidc_client_secret = {
      file = hefe.ops.secrets."gotosocial_oidc_client_secret.age";
      owner = "authelia-main";
      group = "authelia-main";
    };
  };

  services.authelia.instances.main.settings.identity_providers.oidc.clients = [
    {
      client_id = "gotosocial";
      client_secret = "{{ secret \"${config.age.secrets.gotosocial_oidc_client_secret.path}\" }}";
      public = false;
      authorization_policy = "two_factor";
      redirect_uris = [ "https://social.emile.space/auth/callback" ];
      scopes = [
        "openid"
        "email"
        "profile"
        "groups"
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
