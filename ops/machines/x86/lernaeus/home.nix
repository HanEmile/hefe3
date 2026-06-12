# home-manager wiring for lernaeus.
#
# ops/nixos.nix splices in `${home-manager}/nixos` + this file whenever a
# host directory contains a home.nix. This file is where the host opts into
# home-manager and names its user(s) - keeping "emile" out of the shared
# ops/nixos.nix machinery. Mirrors the darwin wiring in ops/darwin.nix.
{ ... }:

{
  home-manager = {
    useGlobalPkgs = true;
    useUserPackages = true;
    users.emile = import ./home_emile.nix;
    sharedModules = [ { home.stateVersion = "25.05"; } ];
  };
}
