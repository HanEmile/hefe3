{ pkgs ? import <nixpkgs> { }, ... }:

let
  inherit (pkgs) sbcl callPackage;
  inherit (sbcl.withPackages (ps: with ps; [ alexandria ])) lispLibs;
in
sbcl.buildASDFSystem {
  pname = "borda-count";
  version = "0.0.2";
  src = pkgs.fetchFromGitHub {
    owner = "kisp";
    repo = "borda-count";
    rev = "56a9c2d1137c3cefb2bfa9b637a073467fcf88f6";
    hash = "sha256-8D8qs4TeNTU4Qp9JP2qnx0vqjPPRQeI374awgAngwxI=";
  };

  systems = [ "borda-count" ];

  lispLibs = lispLibs;
}
