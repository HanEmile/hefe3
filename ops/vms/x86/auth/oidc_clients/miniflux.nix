{ config, hefe, ... }:

{
  age.secrets = {
    miniflux_oidc_client_secret = {
      file = hefe.ops.secrets."miniflux_oidc_client_secret.age";
      owner = "authelia-main";
      group = "authelia-main";
    };
  };

  services.authelia.instances.main.settings.identity_providers.oidc.clients = [
    {
      client_id = "miniflux";
      client_secret = "{{ secret \"${config.age.secrets.miniflux_oidc_client_secret.path}\" }}";
      public = false;
      authorization_policy = "two_factor";
      redirect_uris = [
        "https://rss.pinto-pike.ts.net/oauth2/oidc/callback"
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
