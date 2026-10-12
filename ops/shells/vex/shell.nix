# Ad-hoc vulnerability scanning: SBOM generation + grype with the per-host
# OpenVEX feed (see "Vulnerability scanning" in README.md).
#
#   nix-shell ops/shells/vex
#
let
	sources = import ../../../third_party/npins;
	pkgs = import sources."nixos-26.05" {};
in
 pkgs.mkShell {
 	packages = [
 		pkgs.sbomnix
 		pkgs.grype
 		pkgs.syft
 	];
 }
