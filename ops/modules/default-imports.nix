{ ... }:

# Default set of modules that are imported in all Hefe nixos systems
#
# All modules here should be properly gated behind a `lib.mkEnableOption` with a
# `lib.mkIf` for the config.

{
  imports = [
    ./ports.nix
    ./makhor.nix
  ];
}
