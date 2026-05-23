{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "rss";
  system.stateVersion = "25.05";

  # miniflux + OIDC + backups + healthProbes wired in pass 2 once the host
  # key has been registered in ops/secrets/secrets.nix.
}
