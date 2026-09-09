{ hefe, ... }:

let
	sources = hefe.third_party;
	nixos = sources."nixos-26.05";
	pkgs = import nixos { };
in
	pkgs.callPackage slrn/default.nix {}

