let
	sources = import ../../../third_party/npins;
	pkgs = import sources."nixos-26.05" {};
in
 pkgs.mkShell {
 	packages = [
 		(pkgs.callPackage ./npins.nix {})
 	];
 }
