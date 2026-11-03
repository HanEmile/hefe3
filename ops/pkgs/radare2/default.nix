{ pkgs, ... }:

# Buildable derivation; `nix-build -A ops.pkgs.radare2` works directly.
#
# The plain callPackage-able function is kept in ./package.nix (not read
# by readTree) for building radare2 against other package sets, e.g.:
#   unstable.callPackage (hefe.path + "/ops/pkgs/radare2/package.nix") { }
#
# Update to the latest upstream release with ./update.sh (builds the result
# and rolls back on failure), or via //ops/pkgs/update_all.sh.
pkgs.callPackage ./package.nix { }
