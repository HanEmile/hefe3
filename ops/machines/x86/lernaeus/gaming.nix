# Gaming stack for lernaeus: Steam + Proton + gamescope + gamemode +
# MangoHud + Vulkan tooling.
#
# Steam runs as a normal app inside sway (see sway.nix). The 32-bit GL/
# Vulkan libs Steam/Proton need come from hardware.graphics.enable32Bit in
# gpu.nix. gamescope provides a micro-compositor that smooths over a lot of
# Wayland/NVIDIA quirks for individual games.
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  programs.steam = {
    enable = true;
    # gamescopeSession lets you launch Steam Big Picture inside a gamescope
    # session from the desktop (couch-mode without a dedicated session).
    gamescopeSession.enable = true;
    # Open ports for Steam Remote Play + in-home streaming + friends.
    remotePlay.openFirewall = true;
    localNetworkGameTransfers.openFirewall = true;
    # Proton-GE for broader game compatibility.
    extraCompatPackages = [ pkgs.proton-ge-bin ];
  };

  programs.gamescope = {
    enable = true;
    capSysNice = true; # let gamescope raise its scheduling priority
  };

  # Fix "bwrap: setuid use of bubblewrap is not supported in this build".
  # Something in the closure installs a SETUID /run/wrappers/bin/bwrap, and
  # Steam's launcher picks that up. A setuid bwrap built without setuid
  # support refuses to run. Modern kernels support unprivileged user
  # namespaces (this box has user.max_user_namespaces > 0), so bwrap does
  # NOT need to be setuid - drop the setuid bit so the userns code path is
  # used instead.
  security.wrappers.bwrap = lib.mkForce {
    setuid = false;
    owner = "root";
    group = "root";
    source = "${pkgs.bubblewrap}/bin/bwrap";
  };

  programs.gamemode.enable = true;

  environment.systemPackages = with pkgs; [
    mangohud # in-game FPS/perf overlay (MANGOHUD=1 or via Steam launch opts)
    vulkan-tools # vulkaninfo, vkcube
    vulkan-validation-layers
    vulkan-loader
    protontricks
    winetricks
    wineWow64Packages.stable # 32+64-bit wine (for non-Steam Windows apps)
    lutris # optional non-Steam game launcher
    dxvk # DirectX -> Vulkan
  ];
}
