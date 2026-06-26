# Home Assistant Core, migrated from a 1GB Raspberry Pi (HAOS) that was OOM-ing.
# Runs natively (no Supervisor / add-ons). The config dir was copied wholesale
# from the Pi, so the NixOS module must not manage it (config = null below).
#
# Two-layer function: readTree passes hefe/pkgs/lib, the module system passes
# config. Imported from default.nix as `(import ./homeassistant.nix (args1 // args2))`.
{ hefe, pkgs, lib, ... }:

{ config, ... }:

let
  haStateDir = "/data/private/homeassistant";
in
{
  systemd.tmpfiles.rules = [
    "d ${haStateDir} 0750 hass hass - -"
  ];

  # Required to open the SONOFF Zigbee serial dongle.
  users.users.hass.extraGroups = [ "dialout" ];

  services.home-assistant = {
    enable = true;
    configDir = haStateDir;

    # Only integrations carrying native deps need listing; default_config pulls
    # in the rest. configuration.yaml lists default_config's members minus the
    # Supervisor-coupled ones (cloud/backup/hassio).
    extraComponents = [
      "default_config"
      "zha"
      "apple_tv"
      "wled"
      "yeelight"
      "xiaomi_ble"
      "bluetooth"
      "ibeacon"
      "homekit"
      "homekit_controller"
      "upnp"
      "ipp"
      "brother"
      "go2rtc"
      "met"
      "radio_browser"
      "shopping_list"
      "google_translate"
    ];

    extraPackages = python3Packages: with python3Packages; [ ];

    # Config lives in the migrated configDir; don't let the module regenerate it.
    config = null;
    lovelaceConfig = null;
  };

  # The go2rtc integration needs a running go2rtc; HA reaches it on :1984.
  services.go2rtc.enable = true;

  # Expose HA on its own tailnet hostname via a Tailscale service advertised by
  # this node (svc:homeassistant -> homeassistant.pinto-pike.ts.net, auto TLS).
  # Requires the service to be defined and the host approved in the admin
  # console. A second tailscaled instead of a service breaks MagicDNS, so don't.
  systemd.services.ha-tailscale-service = {
    description = "Advertise Home Assistant as the svc:homeassistant Tailscale service";
    wantedBy = [ "multi-user.target" ];
    after = [ "tailscaled.service" "home-assistant.service" ];
    requires = [ "tailscaled.service" ];
    path = [ pkgs.tailscale ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    script = ''
      set +e
      for i in $(seq 1 30); do
        tailscale status >/dev/null 2>&1 && break
        sleep 2
      done
      tailscale serve --service=svc:homeassistant --bg --https=443 127.0.0.1:8123
      exit 0
    '';
  };
}
