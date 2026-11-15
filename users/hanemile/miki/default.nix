{ hefe, system, ... }:

let
  sources = hefe.third_party;
  nixos = sources."nixos-25.11";
  pkgs = import nixos {
    # system = "x86_64-linux";
    inherit system;
  };
in
{
  pkg = pkgs.callPackage ./package.nix { };
}
