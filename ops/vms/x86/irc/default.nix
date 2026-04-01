{
  hefe,
  pkgs,
  lib,
  ...
}:
{ ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "irc";

  networking.firewall.allowedTCPPorts = [ 6697 ];
  networking.firewall.allowedUDPPorts = [ 6697 ];

  users.users.soju = {
    isSystemUser = true;
    group = "soju";
  };
  users.groups.soju = { };

  age = {
    secrets =
      let
        withSoju =
          x:
          x
          // {
            owner = "soju";
            group = "soju";
          };
      in
      {
        irc_tls_crt = withSoju {
          file = hefe.ops.secrets."irc_tls_crt.age";
        };
        irc_tls_key = withSoju {
          file = hefe.ops.secrets."irc_tls_key.age";
        };
      };
    identityPaths = [
      "/etc/ssh/ssh_host_ed25519_key"
    ];
  };

  systemd.services.soju = {
    serviceConfig = {
      User = "soju";
      Group = "soju";
      DynamicUser = lib.mkForce false;
    };
  };

  services = {
    # ; sojudb -config /nix/store/077lh4fwy1r5ix0xqqd9h1z9plk0hasz-soju.conf create-user hanemile -admin
    #
    # In soju:
    # > /msg BounceServ network create -addr irc.libera.chat -name libera -realname "Emile"
    # > /msg BounceServ network create -addr irc.hackint.org -name hackint -realname "Emile"
    #
    # > /msg NickServ regain hanemile <password>
    soju = {
      enable = true;
      package = pkgs.soju;
      listen = [ "0.0.0.0:6697" ];
      adminSocket.enable = false; # sojuctl at /run/soju/admin

      # TODO(emile): Manually updating these is a pain, but nginx can't
      # forward the requests to be terminated in the VM, will have to find a
      # better solution (such as storing the certs on a ZFS volume, exposing
      # that into the VM via NFS and reading from there)
      tlsCertificate = "/run/agenix/irc_tls_crt";
      tlsCertificateKey = "/run/agenix/irc_tls_key";

      httpOrigins = [ "irc.medano.emile.space" ];
      hostName = "irc.medano.emile.space";
      extraConfig = '''';
      enableMessageLogging = true;
    };
  };

  system.stateVersion = "25.05";
}
