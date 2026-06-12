# Wayland + sway for lernaeus (gaming desktop) - SYSTEM-LEVEL bits only.
#
# The sway *config* (terminal, launcher, keybindings, kitty) lives in
# home-manager (home_emile.nix), which is the only idiomatic way to set it:
# the NixOS `programs.sway` module has no terminal/menu/keybinding options,
# so we used to hack /etc/sway/config.d drop-ins, which fought the stock
# config and raised override warnings. home-manager writes a complete
# ~/.config/sway/config that sway loads instead of /etc/sway/config.
#
# This file keeps the things that genuinely belong to the system: enabling
# sway (session entry + wrappers + the NVIDIA --unsupported-gpu launch flag,
# since home-manager runs with package=null and uses THIS sway binary),
# greetd autologin, polkit/dbus/xwayland, and fonts.
#
# Adapted from https://michael.stapelberg.ch/posts/2026-01-04-wayland-sway-in-2026/
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  user = "emile";
in
{
  programs.sway = {
    enable = true;
    wrapperFeatures.gtk = true;
    # wlroots refuses the NVIDIA proprietary driver unless told to. This
    # flag lives on the system sway binary (home-manager uses package=null,
    # so the session runs THIS binary).
    extraOptions = [ "--unsupported-gpu" ];

    # NVIDIA + wlroots env, exported JUST BEFORE sway starts. This is the
    # reliable place for variables sway itself must see: greetd launches
    # `sway` directly (not via a login shell), so home.sessionVariables are
    # NOT guaranteed to reach the compositor - extraSessionCommands are.
    extraSessionCommands = ''
      export GBM_BACKEND=nvidia-drm
      export __GLX_VENDOR_LIBRARY_NAME=nvidia
      # NVIDIA HW cursor on Wayland is broken -> cursor lag/stutter.
      export WLR_NO_HARDWARE_CURSORS=1
      # Low-latency: cap pre-rendered frames 2->1 (cuts input lag/jitter).
      export __GL_MaxFramesAllowed=1
      # Allow G-Sync/VRR for GL clients (no-op without a VRR output; ignored
      # by Vulkan/Proton, harmless).
      export __GL_GSYNC_ALLOWED=1
      export __GL_VRR_ALLOWED=1
      # NOTE: explicit sync is ENABLED (no WLR_RENDER_NO_EXPLICIT_SYNC).
      # We previously disabled it to dodge a SIGABRT in
      # wlr_linux_drm_syncobj_v1_state_signal_release_with_buffer, but that
      # caused XWayland rendering artifacts on NVIDIA (missing tiles /
      # overlapping text in games, e.g. Factorio zoomed out). The crash is a
      # wlroots NULL-deref fixed in wlroots 0.19.1 (commit d092e40d); the
      # pinned nixpkgs already ships wlroots_0_19 = 0.19.3, so explicit sync
      # is safe to keep on. If sway aborts again on this exact frame,
      # re-capture a backtrace (it would be a newer, different bug).
    '';
  };

  # Autologin straight into sway via greetd. No graphical greeter, no GNOME.
  # greetd runs the sway session as `${user}` on vt1 at boot. sway picks up
  # ~/.config/sway/config written by home-manager.
  services.greetd = {
    enable = true;
    settings = {
      initial_session = {
        command = "sway";
        inherit user;
      };
      default_session = {
        # Fallback interactive greeter if autologin is disabled / not $user.
        command = "${pkgs.tuigreet}/bin/tuigreet --time --cmd sway";
        user = "greeter";
      };
    };
  };

  # Polkit + dbus so GUI apps (Steam, etc.) behave under sway.
  security.polkit.enable = true;
  services.dbus.enable = true;

  # XWayland for X11-only games/apps (Steam itself, many titles).
  programs.xwayland.enable = true;

  fonts.packages = with pkgs; [
    dejavu_fonts
    noto-fonts
    liberation_ttf
  ];
}
