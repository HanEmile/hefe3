# vokobe - minimal static site generator (Rust), built via crane.
{ pkgs, lib ? pkgs.lib, ... }:

let
  package = pkgs.rustPlatform.buildRustPackage {
    pname = "vokobe";
    version = "0.1.3";
    src = lib.cleanSource ./.;
    cargoLock.lockFile = ./Cargo.lock;
    meta = {
      description = "minimal static site generator";
      mainProgram = "vokobe";
    };
  };
in
{
  inherit package;
}
