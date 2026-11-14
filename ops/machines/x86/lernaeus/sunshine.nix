# Sunshine game-streaming host for lernaeus (stream to Moonlight clients,
# e.g. caladan over LAN/Tailscale).
#
# The NixOS module runs sunshine as a systemd *user* service wanted by
# graphical-session.target, which emile's autologin sway session (see
# sway.nix + home_emile.nix HM systemd integration) already activates.
#
# Capture path on this box (sway/wlroots + NVIDIA proprietary, no X11):
# KMS grabber, which needs CAP_SYS_ADMIN (capSysAdmin) plus /dev/dri
# access (emile is in the `video` group) and nvidia-drm modeset=1 (set in
# gpu.nix). Input injection goes through /dev/uinput: the module enables
# hardware.uinput and ships sunshine's uaccess udev rules; emile is in
# `input` and is the active-seat user, so uaccess grants it.
#
# Admin web UI: https://lernaeus:47990 (first-run asks for credentials;
# or set them with `sunshine --creds <user> <pass>` and restart the user
# service). Moonlight pairing: the PIN shown by the client is entered in
# the web UI's "PIN" tab. If avahi auto-discovery doesn't find the host
# (mDNS/UDP 5353 is not opened by services.sunshine.openFirewall), add it
# in Moonlight manually by IP/hostname.
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  services.sunshine = {
    enable = true;
    # Rebuild with CUDA support: the stock nixpkgs build uses
    # -DSUNSHINE_ENABLE_CUDA=FALSE, which still ships the NVENC encoders but
    # NOT the CUDA frame-scaling/colour-conversion path, so every NVENC
    # probe dies with "Couldn't scale frame: Invalid argument" and sunshine
    # silently falls back to software x264. With cudaSupport the probe finds
    # h264/hevc/av1_nvenc on the A2000. (Recipe confirmed against the same
    # nixpkgs sunshine version by naps62/nixos-config commit a273e10.)
    package = pkgs.sunshine.override { cudaSupport = true; };
    # Started by (and bound to) emile's graphical session; stops with it.
    autoStart = true;
    # KMS grabber would need CAP_SYS_ADMIN, but on sway sunshine captures
    # via wlroots screencopy ("Screencasting with Wayland's protocol"),
    # which needs NO privileges. Keep caps OFF: with the capability wrapper
    # glibc runs in secure-execution mode and ignores LD_LIBRARY_PATH,
    # which then breaks libcuda loading (NVENC) via ffmpeg.
    capSysAdmin = false;
    # Sunshine base port 47989: TCP 47984/47989/47990/48010, UDP
    # 47998-48000/48002/48010. Opened on ALL interfaces - LAN-only box, and
    # this keeps it reachable over tailscale0 too.
    openFirewall = true;
  };

  # Admin web UI exposure: nginx on the tailscale IP only, terminating TLS
  # with a tailscale-issued cert (auto-renewed weekly by the shared
  # tailscale-cert-renew module, certs in /var/lib/tailscale-certs),
  # proxying to Sunshine's self-signed https UI on localhost:47990.
  # Reach it from the tailnet as https://lernaeus.pinto-pike.ts.net/
  # (no cert warning, unlike :47990's self-signed cert). Pairing flow:
  # Moonlight shows a PIN -> enter it in the web UI's PIN tab.
  #
  # NOTE: Sunshine's CSRF protection rejects non-default origins; the proxy
  # origin was allowed once in the STATEFUL config
  # (~emile/.config/sunshine/sunshine.conf):
  #   csrf_allowed_origins = https://lernaeus.pinto-pike.ts.net
  # Deliberately not in services.sunshine.settings: settings render into a
  # Nix config file and freeze ALL web-UI configuration (incl. pairing).
  tailscaleCertRenew.hostnames = [ "lernaeus.pinto-pike.ts.net" ];

  networking.firewall.interfaces."tailscale0".allowedTCPPorts = [ 443 ];

  services.nginx = {
    enable = true;
    recommendedProxySettings = true;
    virtualHosts."lernaeus.pinto-pike.ts.net" = {
      # Needed for the ssl_certificate lines to be emitted at all; the
      # custom `listen` below alone (ssl=true) doesn't trigger it.
      onlySSL = true;
      sslCertificate = "/var/lib/tailscale-certs/lernaeus.pinto-pike.ts.net.crt";
      sslCertificateKey = "/var/lib/tailscale-certs/lernaeus.pinto-pike.ts.net.key";
      # Bind only the tailscale interface address; no plain-http vhost.
      listen = [
        {
          addr = "100.122.98.27";
          port = 443;
          ssl = true;
        }
      ];
      locations."/" = {
        proxyPass = "https://127.0.0.1:47990";
        proxyWebsockets = true; # UI uses websockets for live logs/pairing
        extraConfig = ''
          proxy_ssl_verify off; # upstream is Sunshine's self-signed cert
        '';
      };
    };
  };

  # NVENC probing fails with "Cannot load libcuda.so.1" without this: the
  # dlopen happens inside ffmpeg and NixOS puts no global libdir on the
  # search path. Works only because capSysAdmin=false (see above) - with
  # the capability wrapper, glibc secure mode ignores LD_LIBRARY_PATH.
  systemd.user.services.sunshine.serviceConfig.Environment = [
    "LD_LIBRARY_PATH=/run/opengl-driver/lib"
  ];
}
