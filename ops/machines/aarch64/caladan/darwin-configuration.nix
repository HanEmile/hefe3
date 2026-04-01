{ pkgs ? <nixpkgs>
, lib ? pkgs.lib
, ... }:

{
  imports = [
    ./overlay.nix
  ];

  system.stateVersion = 5;

  users.users.emile = {
    name = "emile";
    home = "/Users/emile";
  };

  users.users.hydra = {
    name = "hydra";
    home = "/Users/hydra";
  };

  ids.gids.nixbld = 30000;

  nix = {
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
        "hydra"
        "emile"
      ];

      trusted-public-keys = [
        "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
        "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
        "cache.garnix.io:CTFPyKSLcx5RMJKfLo5EEPUObbA78b0YQ2DTCJXqr9g="
        # "nix-cache.emile.space:3xzJknXMsR/EL3SBTu6V6oCOkjxe6MgJm0nOrElW33A="
      ];
      substituters = [
        # nix-cache mirror for when in china

        "https://cache.nixos.org"
        "https://nix-community.cachix.org"
        "https://cache.garnix.io"
        # "https://nix-cache.emile.space"

        # status: https://mirror.sjtu.edu.cn/
        "https://mirror.sjtu.edu.cn/nix-channels/store"

        # status: https://mirrors.ustc.edu.cn/status/
        "https://mirrors.ustc.edu.cn/nix-channels/store"

      ];

      experimental-features = [
        "nix-command"
        "flakes"
      ];

      # don't use the globally defined flakes, as pulling from github for each shell invocation
      # is slow
      flake-registry = "";
    };

    distributedBuilds = true;

    buildMachines = [
      # Feature	        | Derivations requiring it
      # ----------------|-----------------------------------------------------
      # kvm	            | Everything which builds inside a vm, like NixOS tests
      # nixos-test	    | Machine can run NixOS tests
      # big-parallel    | kernel config, libreoffice, evolution, llvm and chromium.
      # benchmark	      | Machine can generate metrics (Means the builds usually
      #                 | takes the same amount of time)

      # cat /etc/nix/machines
      # root@corrino  x86_64-linux      /home/nix/.ssh/id_ed25519        8 1     kvm,benchmark

      {
        hostName = "corrino.emile.space";
        system = "x86_64-linux";
        maxJobs = 10;
        speedFactor = 2;

        supportedFeatures = [
          "nixos-test"
          "benchmark"
          "big-parallel"
          "kvm"
        ];
        mandatoryFeatures = [ ];
      }
      # {
      #   hostName = "medano.emile.space";
      #   system = "x86_64-linux";
      #   maxJobs = 10;
      #   speedFactor = 2;

      #   supportedFeatures = [
      #     "nixos-test"
      #     "benchmark"
      #     "big-parallel"
      #     "kvm"
      #   ];
      #   mandatoryFeatures = [ ];
      # }
    ];
  };

  nixpkgs.config = {
    allowUnfree = true;
    allowUnsupportedSystem = true;
  };

  # <3
  # security.pam.enableSudoTouchIdAuth = true;
  security.pam.services.sudo_local.touchIdAuth = true;

  environment = {
    systemPackages = [ ]; # set via home-manager
    shells = with pkgs; [
      bashInteractive
      zsh
    ];
  };

  system.primaryUser = "emile";
}
