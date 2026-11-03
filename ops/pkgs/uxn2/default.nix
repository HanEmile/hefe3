{ pkgs, ... }:

# Buildable derivation; `nix-build -A ops.pkgs.uxn2` works directly.
#
# The plain callPackage-able function is kept in ./package.nix (not read
# by readTree) for building uxn2 against other package sets, e.g.:
#   unstable.callPackage (hefe.path + "/ops/pkgs/uxn2/package.nix") { }
pkgs.callPackage ./package.nix { }
