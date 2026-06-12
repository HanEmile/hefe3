# GPIO-based power control for BMC Pis.
#
# Exposes a systemd service that can pulse the power/reset GPIO pins to
# control the paired PC. The default pin assignments assume: GPIO17 ->
# PC power button header, GPIO27 -> PC reset button header. Override via
# the options below in per-host configs.
#
# Usage:
#   systemctl start bmc-power-on     # pulse power button (short press)
#   systemctl start bmc-power-off    # hold power button (5s, force off)
#   systemctl start bmc-reset        # pulse reset button
{ lib, pkgs, config, ... }:

let
  cfg = config.services.bmc-power;
in
{
  options.services.bmc-power = {
    enable = lib.mkEnableOption "BMC GPIO power control";

    powerGpio = lib.mkOption {
      type = lib.types.int;
      default = 17;
      description = "GPIO pin connected to the PC's power button header.";
    };

    resetGpio = lib.mkOption {
      type = lib.types.int;
      default = 27;
      description = "GPIO pin connected to the PC's reset button header.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services."bmc-power-on" = {
      description = "BMC: pulse PC power button (short press)";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = pkgs.writeShellScript "bmc-power-on" ''
          ${pkgs.libgpiod}/bin/gpioset -m time -u 500000 gpiochip0 ${toString cfg.powerGpio}=1
        '';
      };
    };

    systemd.services."bmc-power-off" = {
      description = "BMC: hold PC power button (force off, 5s)";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = pkgs.writeShellScript "bmc-power-off" ''
          ${pkgs.libgpiod}/bin/gpioset -m time -u 5000000 gpiochip0 ${toString cfg.powerGpio}=1
        '';
      };
    };

    systemd.services."bmc-reset" = {
      description = "BMC: pulse PC reset button";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = pkgs.writeShellScript "bmc-reset" ''
          ${pkgs.libgpiod}/bin/gpioset -m time -u 500000 gpiochip0 ${toString cfg.resetGpio}=1
        '';
      };
    };
  };
}
