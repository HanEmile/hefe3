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
    fzf.enableZshIntegration = true;

    zsh = {
      enable = true;
      enableCompletion = true;
      autocd = true;
      autosuggestion.enable = true;
      history.append = true;
      # syntaxHighlighting.enable = true;
      shellAliases = import ./aliases.nix;
      # autosuggestions.enable = true;
      # enableAutosuggestions = true;

      oh-my-zsh = {
        enable = true;
        plugins = [
          "git"
          "web-search"
          "urltools"
          "fzf"
        ];
      };

      defaultKeymap = "viins";

      # This has to be added, so we can ssh into the host using deploy-rs and
      # access the `nix-store` stuff
      envExtra = ''
        if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
          . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
        fi
      '';

      initContent = lib.mkOrder 550 ''
        ${builtins.readFile ./session_variables.zsh}

        eval "$(direnv hook zsh)"

        setopt autocd 		# cd without needing to use the cd command

        # history related
        setopt INC_APPEND_HISTORY # append history directly, not when closing the shell
        setopt SHARE_HISTORY
        setopt AUTO_PUSHD
      '';
    };

    kitty = {
      enable = true;

      font = {
        name = "'Berkeley Mono'";
        size = 13;
      };

      shellIntegration.enableZshIntegration = true;

      settings = {
        disable_ligatures = "never";
        close_on_child_death = "yes";

        tab_bar_edge = "top";
        tab_bar_style = "slant";
        tab_bar_min_tabs = 1;
        tab_title_template = "{index} {title.replace('emile', 'e')}";

        editor = "/Users/emile/.nix-profile/bin/hx";

        macos_option_as_alt = "yes"; # orig: no, just trying this out
        macos_quit_when_last_window_closed = "yes";

        kitty_mod = "ctrl+shift";

        allow_remote_control = "yes";
      };

      keybindings = {
        "cmd+enter" = "launch --cwd=current --location=split";
        "cmd+shift+enter" = "launch --cwd=current --location=hsplit";

        "cmd+shift+h" = "move_window left";
        "cmd+shift+j" = "move_window down";
        "cmd+shift+k" = "move_window up";
        "cmd+shift+l" = "move_window right";

        "command+j" = "kitten pass_keys.py neighboring_window bottom command+j";
        "command+k" = "kitten pass_keys.py neighboring_window top    command+k";
        "command+h" = "kitten pass_keys.py neighboring_window left   command+h";
        "command+l" = "kitten pass_keys.py neighboring_window right  command+l";
        "command+b" = "combine : clear_terminal scroll active : send_text normal,application \x0c";
      };

      environment = { };
    };
  };

  home.packages = with pkgs; [

    # terminal
    coreutils
    mktemp
    rlwrap

    # filesystem
    fd
    eza
    tree
    ripgrep
    entr

    # filesystem size analysis
    broot
    dust

    # viewing stuff
    jq
    bat
    graphviz
    fzf


    # system resource analysis
    htop

    # getting stuff from a to b
    rsync
    lftp
    aria2

    # unpacking stuff
    p7zip
    binwalk

    # file manipulation
    imagemagick
    ffmpeg
    exiftool
    mpv

    # VCS
    tig

    # nix related tools
    nixos-rebuild
    agenix

    # editor
    unstable.helix

    ## formatter
    nixfmt-rfc-style # official formatter for nix code

    ## language server
    # nodePackages_latest.typescript-language-server # js / typescript
    nil # nix
    nixd # nix
    # nodePackages.yaml-language-server # yaml
    python313Packages.python-lsp-server # python
    marksman # markdown lsp

    # binary

    # network
    curl
    wireguard-tools
    unstable.tailscale

    # rss
    yarr

    # go
    go
    gotools
    gopls
    golangci-lint
    golangci-lint-langserver # golang

    # c
    cmake
    pkg-config

    # iot hack
    minicom

    SDL2

    # qemu tooling
    qemu
    sphinx # docs
    virt-manager

    # lisp
    sbcl

    # infrastructure as code
    terraform

    portmidi

    tiny # irc

    # python313 (yes, I use requests a lot and can't bother creating a new nix-shell for each time
    # I need it), btw.: a minimal shell would look like this:
    #
    # { pkgs ? import <nixpkgs> {} }:
    # pkgs.mkShell {
    #   buildInputs = [
    #     (pkgs.python3.withPackages (p: with p; [ requests]))
    #   ];
    # }
    (pkgs.python313.withPackages (
      ps: with ps; [
        requests
        z3-solver
      ]
    ))

    z3 # theorem prover

    taskwarrior3

    # rust
    cargo
    rust-analyzer

    # vms
    utm

    # discovery tools
    nmap
    ffuf

    # typesetting
    typst

    # crypto
    age
    gnupg

    # java
    jre_minimal

    # getting out of networks
    iodine

    # reversing
    ghidra
    radare2

    # file sync
    syncthing
    git-annex

    senpai # irc

    unstable.zed-editor # editor

    # ai
    gemini-cli
    codex
    claude-code

    package-version-server

    npins

    # custom
    # libc-database

    # blender

    # rustdesk

    # ] ++ lib.optionals stdenv.isDarwin [
  ];
}
