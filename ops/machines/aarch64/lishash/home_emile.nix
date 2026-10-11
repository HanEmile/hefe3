{ config, pkgs, lib, ... }:

{
  home = {
    # The state version is required and should stay at the version you
    # originally installed.
    stateVersion = "22.11";
    username = "emile";
    homeDirectory = "/Users/emile";
  };

  programs = {
    # let home-manager install and manage itself
    home-manager.enable = true;

    direnv = {
      enable = true;
      nix-direnv.enable = true;
    };

    fzf = {
      enable = true;
      enableZshIntegration = true;
    };

    git = {
      signing.key = "${config.home.homeDirectory}/.ssh/ed25519.pub";
      signing.signByDefault = true;
      settings.gpg.format = "ssh";
    };

    htop = {
      enable = true;
      settings.show_program_with_path = true;
    };

    eza.enableZshIntegration = true;

    zsh = {
      enable = true;
      enableCompletion = true;
      autocd = true;
      autosuggestion.enable = true;
      history.append = true;

      oh-my-zsh = {
        enable = true;
        plugins = [
          "git"
          "fzf"
        ];
      };

      # This has to be added, so we can ssh into the host using deploy-rs and
      # access the `nix-store` stuff
      envExtra = ''
        if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
          . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
        fi
      '';

      initContent = lib.mkOrder 550 ''
        eval "$(direnv hook zsh)"

        setopt autocd 		# cd without needing to use the cd command

        # history related
        setopt INC_APPEND_HISTORY # append history directly, not when closing the shell
        setopt SHARE_HISTORY
        setopt AUTO_PUSHD
      '';
    };
  };

  # Minimal headless box: CLI tools only, no GUI apps (kitty, editors with
  # UI chrome, etc.)
  home.packages = with pkgs; [
    coreutils
    mktemp
    rlwrap

    fd
    eza
    tree
    ripgrep
    entr

    jq
    bat

    htop

    rsync

    nixos-rebuild
    ragenix
    npins

    helix
    nixfmt
    nil
    nixd

    curl
    google-chrome
    tailscale
  ];
}
