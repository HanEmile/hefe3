{ config, pkgs, lib, unstable, ... }:

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

    aerospace = {
      enable = false;
      launchd.enable = true;
      userSettings = {
        #persistent-workspaces = [];

        # i3 doesn't have "normalizations" feature that why we disable them here.
        # But the feature is very helpful.
        # Normalizations eliminate all sorts of weird tree configurations that don't make sense.
        # Give normalizations a chance and enable them back.
        enable-normalization-flatten-containers = false;
        enable-normalization-opposite-orientation-for-nested-containers = false;

        # Mouse follows focus when focused monitor changes
        # on-focused-monitor-changed = ["move-mouse monitor-lazy-center"];

        gaps = {
          outer.left = 0;
          outer.bottom = 0;
          outer.top = 0;
          outer.right = 0;
        };

        mode.main.binding = {
          alt-enter = "exec-and-forget open -na kitty";
          alt-shift-q = "close";
          
          alt-h = "focus --boundaries-action wrap-around-the-workspace left";
          alt-j = "focus --boundaries-action wrap-around-the-workspace down";
          alt-k = "focus --boundaries-action wrap-around-the-workspace up";
          alt-l = "focus --boundaries-action wrap-around-the-workspace right";


          alt-shift-h = "move left";
          alt-shift-j = "move down";
          alt-shift-k = "move up";
          alt-shift-l = "move right";

          alt-v = "split vertical";
          alt-b = "split horizontal";

          alt-f = "fullscreen";

          alt-s = "layout v_accordion";
          alt-w = "layout h_accordion";
          alt-e = "layout tiles horizontal vertical";

          alt-1 = "workspace 1";
          alt-2 = "workspace 2";
          alt-3 = "workspace 3";
          alt-4 = "workspace 4";
          alt-5 = "workspace 5";
          alt-6 = "workspace 6";
          alt-7 = "workspace 7";
          alt-8 = "workspace 8";
          alt-9 = "workspace 9";

          alt-shift-1 = "move-node-to-workspace 1";
          alt-shift-2 = "move-node-to-workspace 2";
          alt-shift-3 = "move-node-to-workspace 3";
          alt-shift-4 = "move-node-to-workspace 4";
          alt-shift-5 = "move-node-to-workspace 5";
          alt-shift-6 = "move-node-to-workspace 6";
          alt-shift-7 = "move-node-to-workspace 7";
          alt-shift-8 = "move-node-to-workspace 8";
          alt-shift-9 = "move-node-to-workspace 9";

          alt-shift-c = "reload-config";

          alt-r = "mode resize";
        };

        mode.resize.binding = {
          h = "resize width -50";
          j = "resize height +50";
          k = "resize height -50";
          l = "resize width +50";
          enter = "mode main";
          esc = "mode main";
        };
      };
    };

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

      # defaultKeymap = "viins";

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

        macos_option_as_alt = "right";
        # macos_option_as_alt = "none";
        
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
    unstable.mpv

    # VCS
    tig

    # nix related tools
    nixos-rebuild
    ragenix # agenix
    npins

    # editor
    helix

    ## formatter
    # nixfmt-rfc-style # official formatter for nix code
    nixfmt

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
    tailscale

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
    # virt-manager

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

    zed-editor # editor
    # (zed-editor.overrideAttrs (oldAttrs: {
    #   dontStrip = true;
    #   noAuditTmpdir = true;
    # }))


    # ai
    # gemini-cli
    # codex
    claude-code

    package-version-server

    logseq

    # custom
    # libc-database

    # blender

    # rustdesk

    # ] ++ lib.optionals stdenv.isDarwin [
  ];
}
