# home-manager config for emile on lernaeus (sway gaming desktop).
#
# This replaces the earlier environment.etc /etc/sway/config.d hacks: the
# home-manager sway module writes a COMPLETE ~/.config/sway/config that sway
# loads instead of the stock /etc/sway/config, so there is nothing to
# "override" and no duplicate-binding warnings.
{
  config,
  lib,
  pkgs,
  ...
}:

let
  # swaybar status line: CPU temp, GPU temp, eth/wifi IP, disk, RAM, clock.
  # Sensors/interfaces are resolved by KIND (chip name / wireless dir), not
  # fixed hwmonN / ifname paths, since those are not stable across boots.
  statusScript = pkgs.writeShellScript "sway-status" ''
    PATH=${
      lib.makeBinPath [
        pkgs.coreutils
        pkgs.gnused
        pkgs.gawk
        pkgs.iproute2
        pkgs.procps
      ]
    }:$PATH

    # First IPv4 on the wireless interface (found via /sys .../wireless).
    wifi_ip() {
      for n in /sys/class/net/*; do
        [ -d "$n/wireless" ] || continue
        ip -4 -o addr show dev "$(basename "$n")" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1
        return
      done
    }
    # First IPv4 on a wired, non-virtual interface (skip wifi/lo/tailscale/
    # bridges/veth by requiring a /device symlink and no /wireless).
    eth_ip() {
      for n in /sys/class/net/*; do
        b=$(basename "$n")
        [ "$b" = "lo" ] && continue
        [ -d "$n/wireless" ] && continue
        [ -e "$n/device" ] || continue
        ip -4 -o addr show dev "$b" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1
        return
      done
    }

    while true; do
      # CPU temp: prefer k10temp Tctl (true die temp). On this ASRock
      # A520M-ITX/ac the CPU's on-die SMU telemetry can WEDGE at the
      # saturation ceiling (95.875C, = max register code) and stop moving -
      # cleared by a FULL power-drain (not a warm reboot), and ultimately a
      # BIOS update (L3.44 -> L3.90). If Tctl reads >=95C we treat it as
      # wedged and fall back to the nct6792 CPUTIN socket thermistor (a
      # separate, independent sensor that stays sane, reads a bit high).
      # Sensors found by chip name / label so this survives hwmon reindexing.
      cpu="?"
      for d in /sys/class/hwmon/hwmon*; do
        if [ "$(cat "$d/name" 2>/dev/null)" = "k10temp" ]; then
          m=$(cat "$d/temp1_input" 2>/dev/null || echo "")
          [ -n "$m" ] && cpu=$(( m / 1000 ))
          break
        fi
      done
      if [ "$cpu" = "?" ] || [ "$cpu" -ge 95 ] 2>/dev/null; then
        for d in /sys/class/hwmon/hwmon*; do
          if [ "$(cat "$d/name" 2>/dev/null)" = "nct6792" ]; then
            for t in "$d"/temp*_input; do
              [ -e "$t" ] || continue
              if [ "$(cat "''${t%_input}_label" 2>/dev/null)" = "CPUTIN" ]; then
                m=$(cat "$t" 2>/dev/null || echo "")
                [ -n "$m" ] && cpu=$(( m / 1000 ))
                break
              fi
            done
            break
          fi
        done
      fi
      # GPU: nvidia-smi
      gpu=$(timeout 3 /run/current-system/sw/bin/nvidia-smi \
              --query-gpu=temperature.gpu --format=csv,noheader,nounits 2>/dev/null \
              | sed 's/[^0-9]//g')
      [ -n "$gpu" ] || gpu="?"

      eth=$(eth_ip); [ -n "$eth" ] || eth="-"
      wifi=$(wifi_ip); [ -n "$wifi" ] || wifi="-"

      # Disk: root filesystem used/size + percent.
      disk=$(df -h / | awk 'NR==2{print $3"/"$2" "$5}')
      # RAM: used/total (human).
      ram=$(free -h | awk '/^Mem:/{print $3"/"$2}')

      date=$(date '+%Y-%m-%d %H:%M')

      echo "CPU ''${cpu}°C | GPU ''${gpu}°C | eth ''${eth} | wifi ''${wifi} | disk ''${disk} | ram ''${ram} | ''${date}"
      sleep 3
    done
  '';
in
{
  home.stateVersion = "25.05";

  wayland.windowManager.sway = {
    enable = true;
    wrapperFeatures.gtk = true;
    # wlroots refuses the NVIDIA proprietary driver without this. 595 +
    # sway 1.11 + wlroots 0.19 support explicit sync, so it is functional.
    extraOptions = [ "--unsupported-gpu" ];
    # Let NixOS (programs.sway) own the sway binary / session entry; HM only
    # writes the config. Avoids the well-known package-shadowing conflict.
    # Trade-off: no auto-reload on activation (run `swaymsg reload` after a
    # deploy, or it picks up on next login).
    package = null;

    config = rec {
      modifier = "Mod4"; # Super
      terminal = "${pkgs.kitty}/bin/kitty";
      menu = "${pkgs.rofi}/bin/rofi -show drun";

      # Merge our keys with the module's sensible i3 defaults. mkOptionDefault
      # keeps every default (focus Mod+hjkl, workspaces Mod+1..0, Mod+Shift+q
      # kill, Mod+f fullscreen, Mod+r resize, ...) and lets these win. Because
      # `terminal`/`menu` above are set, the default Mod+Return / Mod+d already
      # use kitty/rofi; we re-list them only for clarity plus add extras.
      keybindings = lib.mkOptionDefault {
        "${modifier}+Return" = "exec ${terminal}";
        "${modifier}+d" = "exec ${menu}";
        "${modifier}+Shift+x" = "exec ${pkgs.swaylock}/bin/swaylock -f -c 000000";
        # screenshots -> clipboard (wrap pipeline in sh -c)
        "Print" = "exec ${pkgs.bash}/bin/sh -c '${pkgs.grim}/bin/grim -g \"$(${pkgs.slurp}/bin/slurp)\" - | ${pkgs.wl-clipboard}/bin/wl-copy'";
        "${modifier}+Shift+s" = "exec ${pkgs.bash}/bin/sh -c '${pkgs.grim}/bin/grim -g \"$(${pkgs.slurp}/bin/slurp)\" - | ${pkgs.wl-clipboard}/bin/wl-copy'";
      };

      output."*".bg = "#000000 solid_color";

      # DP-3 explicit mode. The monitor (Lenovo S24q-10) is natively
      # 2560x1440, but the GPU is connected through a Mini-DP->HDMI adapter
      # that only negotiates an HDMI-1.4-class link (~148.5MHz TMDS ceiling)
      # -> 1440p (241.5MHz) and 1080p75 are not achievable over it (the
      # driver correctly drops higher modes; verified by forcing a
      # 1440p-only EDID, which it still refused). 1080p60 is the max this
      # link supports. To get 1440p, swap to a 4K60 Mini-DP->HDMI 2.0
      # adapter or Mini-DP->DP, then bump this to 2560x1440@75Hz.
      output."DP-3".mode = "1920x1080@60Hz";

      # Stop screen-blank/DPMS while any window is fullscreen (games).
      # swayidle honors the idle-inhibit protocol, so this is the correct
      # swayidle-aware way - no need to kill the idle daemon.
      window.commands = [
        {
          criteria = { class = ".*"; };
          command = "inhibit_idle fullscreen";
        }
        {
          criteria = { app_id = ".*"; };
          command = "inhibit_idle fullscreen";
        }
      ];

      # Status bar: show CPU temp / GPU temp / clock via our status script
      # instead of the stock i3status.
      bars = [
        {
          position = "bottom";
          statusCommand = "${statusScript}";
          fonts = {
            names = [ "monospace" ];
            size = 11.0;
          };
        }
      ];
    };
  };

  # NVIDIA + Wayland session env. Set in the home session so they apply to
  # sway and everything it launches.
  # App-level Wayland hints (read by the apps themselves at launch, so the
  # home session is the right place). The compositor-critical NVIDIA/wlroots
  # vars (GBM_BACKEND, WLR_*) are exported in programs.sway.extraSessionCommands
  # (sway.nix) instead, because greetd launches sway directly and would not
  # reliably pick these up.
  home.sessionVariables = {
    NIXOS_OZONE_WL = "1"; # Electron/Chromium (incl. some launchers) -> Wayland
    MOZ_ENABLE_WAYLAND = "1";
  };

  # kitty, ported from caladan's programs.kitty
  # (ops/machines/aarch64/caladan/home_emile.nix). macOS-only bits dropped:
  # macos_* options, cmd+ keybindings, the yabai neighboring_window kittens.
  # NOTE: "Berkeley Mono" is a commercial font not packaged in nixpkgs; kitty
  # falls back to its default monospace until the font is installed.
  programs.kitty = {
    enable = true;
    font = {
      name = "Berkeley Mono";
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
      editor = "${pkgs.helix}/bin/hx";
      kitty_mod = "ctrl+shift";
      allow_remote_control = "yes";
    };
    keybindings = {
      "super+enter" = "launch --cwd=current --location=split";
      "super+shift+enter" = "launch --cwd=current --location=hsplit";
      "super+shift+h" = "move_window left";
      "super+shift+j" = "move_window down";
      "super+shift+k" = "move_window up";
      "super+shift+l" = "move_window right";
    };
  };

  # Wayland desktop helpers that used to be in programs.sway.extraPackages
  # but belong to the user's session.
  home.packages = with pkgs; [
    rofi
    wl-clipboard
    grim
    slurp
    mako
    gammastep
    wtype
    wayland-utils
    jq # JSON querying (swaymsg -t get_outputs, general use)
  ];
}
