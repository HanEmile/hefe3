{ pkgs, lib, ... }:

{
  system.stateVersion = 5;

  ids.uids.nixbld = lib.mkForce 350;
  ids.gids.nixbld = lib.mkForce 350;

  users.users.emile = {
    name = "emile";
    home = "/Users/emile";
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPZi43zHEsoWaQomLGaftPE5k0RqVrZyiTtGqZlpWsew emile@caladan"
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEzLZ56SEgwZZ0OusTdSDDhpMlxSg1zPNdRLuxKOfrR5 emile@chusuk"
    ];
  };

  services.openssh.enable = true;

  services.tailscale.enable = true;

  nix = {
    enable = true;
    extraOptions =
      ''
        builders-use-substitutes = true
        auto-optimise-store = true
      ''
      + lib.optionalString (pkgs.system == "aarch64-darwin") ''
        extra-platforms = x86_64-darwin aarch64-darwin
      '';

    settings = {
      trusted-users = [
        "root"
        "emile"
      ];

      trusted-public-keys = [
        "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
        "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
        "cache.garnix.io:CTFPyKSLcx5RMJKfLo5EEPUObbA78b0YQ2DTCJXqr9g="
      ];
      substituters = [
        "https://cache.nixos.org"
        "https://nix-community.cachix.org"
        "https://cache.garnix.io"
      ];

      experimental-features = [
        "nix-command"
        "flakes"
      ];

      # don't use the globally defined flakes, as pulling from github for each shell invocation
      # is slow
      flake-registry = "";
    };
  };

  nixpkgs.config = {
    allowUnfree = true;
    allowUnsupportedSystem = true;
    doCheck = false;
  };

  environment = {
    systemPackages = [ ]; # set via home-manager
    shells = with pkgs; [
      bashInteractive
      zsh
    ];
  };

  system.primaryUser = "emile";
}
