{ pkgs ? import <nixpkgs> { }, ... }:

let
  inherit (pkgs) sbcl callPackage nix-gitignore;
  borda-count = callPackage ./borda-count.nix { };
  inherit
    (sbcl.withPackages (
      ps: with ps; [
        iterate
        borda-count
      ]
    ))
    lispLibs;
in sbcl.buildASDFSystem {
  pname = "custom-lib";
  version = "0.0.1";
  src =
    let
      patterns = ''
        *
        !custom-lib.asd
        !custom-lib.lisp
      '';
    in
    nix-gitignore.gitignoreSourcePure patterns ./.;

  systems = [ "custom-lib" ];

  lispLibs = lispLibs;
}
