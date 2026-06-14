{ pkgs, lib, sources, ... }:


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

    nixPath = [
      "darwin=${sources."nix-darwin".outPath}"
      "nixpkgs=${sources."nixos-unstable".outPath}"
    ];

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

      # {
      #   hostName = "corrino.emile.space";
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
      {
        hostName = "medano.emile.space";
        sshKey = "/Users/emile/.ssh/id_ed25519";
        systems = [ "x86_64-linux" "armv6l-linux" ];
        maxJobs = 10;
        speedFactor = 2;

        supportedFeatures = [
          "nixos-test"
          "benchmark"
          "big-parallel"
          "kvm"
          "gccarch-armv6kz"
          "gccarch-x86-64-v2"
        ];
        mandatoryFeatures = [ ];
      }
      # lernaeus removed as a remote builder for now: the box is off and its
      # LAN hostname (192.168.1.79) is unreachable from caladan, which made
      # every distributed build hang on the SSH connect before falling back
      # to medano. Re-enable once lernaeus is reliably up on the tailnet.
      # {
      #   hostName = "lernaeus";
      #   sshKey = "/Users/emile/.ssh/id_ed25519";
      #   systems = [ "x86_64-linux" "armv6l-linux" ];
      #   maxJobs = 12;
      #   speedFactor = 1;
      #
      #   supportedFeatures = [
      #     "benchmark"
      #     "big-parallel"
      #     "gccarch-armv6kz"
      #     "gccarch-x86-64-v2"
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


  # System-wide SSH known hosts so the nix daemon (root) can reach remote
  # builders without hanging on the host key verification prompt.
  programs.ssh.knownHosts = {
    "medano.emile.space" = {
      publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDo2MqY7BD6rd/L3UURx/2kTuHMC7V7WmW74bCsejChq";
    };
    lernaeus = {
      hostNames = [ "lernaeus" "lernaeus.pinto-pike.ts.net" "192.168.1.79" "100.122.98.27" ];
      publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOASVDM+HusQY7btHM76V0HyllczztxRESaQMnL1PnFi";
    };
  };

  system.primaryUser = "emile";
}
