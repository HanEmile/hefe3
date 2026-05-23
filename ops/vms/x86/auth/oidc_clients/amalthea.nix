{ config, hefe, ... }:

{
  age.secrets."amalthea_oidc_client_secret" = {
    file = hefe.ops.secrets."amalthea_oidc_client_secret.age";
    owner = "authelia-main";
    group = "authelia-main";
  };

  # auth via authelia
  services.authelia.instances.main.settings.identity_providers.oidc.clients = [
    {
      client_id = "amalthea";

      # ; nix run nixpkgs#authelia -- crypto hash generate pbkdf2 --variant sha512 --random --random.length 72 --random.charset rfc3986
      client_secret = "{{ secret \"${config.age.secrets.amalthea_oidc_client_secret.path}\" }}";
      public = false;
      authorization_policy = "two_factor";
      redirect_uris = [
      	"https://amalthea.medano.emile.space/auth/callback"
      	"http://127.0.0.1:19284/callback" # native app fixed port
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
      	"fragement"
      ];
      token_endpoint_auth_method = "client_secret_post";
    }
  ];
}
