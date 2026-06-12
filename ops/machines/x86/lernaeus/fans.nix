# GPU-temperature-driven case fan for lernaeus.
#
# The stock GPU blower is very loud, so a larger CASE fan plugged into the
# motherboard CHA_FAN1 header blows over the GPU instead. This drives that
# case fan from the GPU temperature.
#
# Hardware (probed on the box):
#   SuperIO: Nuvoton NCT6792D  -> hwmon name "nct6792", kernel module nct6775
#   CHA_FAN1 = pwm1 = fan1     (verified: pwm1 255->2292rpm, 60->690rpm;
#                               the CPU fan is pwm2/fan2 and is left alone)
#   GPU temp: nvidia-smi only (not a hwmon sensor).
#
# Implementation: a single self-contained systemd service reads the GPU temp
# via nvidia-smi and writes pwm1 directly per a temp->speed curve. This is
# simpler and more robust than lm_sensors/fancontrol here, because fancontrol
# can only map a PWM to a real hwmon temp input - and the GPU temp isn't one.
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  # temp(C) -> pwm(0-255) curve. Linear ramp between the knees:
  #   <= minTemp -> minPwm (quiet floor; fan still spins ~690rpm at 60)
  #   >= maxTemp -> 255 (full)
  minTemp = 45;
  maxTemp = 75;
  minPwm = 60; # below this the fan may stall; keeps a quiet floor
  curve = pkgs.writeShellScript "gpu-fan-curve" ''
    set -eu
    SMI=/run/current-system/sw/bin/nvidia-smi
    command -v "$SMI" >/dev/null 2>&1 || SMI=$(command -v nvidia-smi || echo nvidia-smi)

    # Resolve the nct6792 hwmon dir by name (hwmon indices are not stable).
    HW=""
    for d in /sys/class/hwmon/hwmon*; do
      if [ "$(cat "$d/name" 2>/dev/null)" = "nct6792" ]; then HW="$d"; break; fi
    done
    if [ -z "$HW" ]; then
      echo "nct6792 hwmon not found (nct6775 loaded?)" >&2; exit 1
    fi
    PWM="$HW/pwm1"

    # Take manual control of pwm1 (mode 1 = manual). Leaves pwm2 (CPU) alone.
    echo 1 > "$HW/pwm1_enable"

    MINT=${toString minTemp}; MAXT=${toString maxTemp}; MINP=${toString minPwm}

    cleanup() {
      # On stop, hand pwm1 back to the BIOS/firmware automatic mode (5)
      # so the fan is never left stuck.
      echo 5 > "$HW/pwm1_enable" 2>/dev/null || true
    }
    trap cleanup EXIT TERM INT

    while true; do
      T=$(${pkgs.coreutils}/bin/timeout 5 "$SMI" \
            --query-gpu=temperature.gpu --format=csv,noheader,nounits 2>/dev/null \
            | ${pkgs.gnused}/bin/sed 's/[^0-9]//g')
      [ -n "$T" ] || T=50   # safe-ish default if query fails (fan ~mid)

      if [ "$T" -le "$MINT" ]; then
        P=$MINP
      elif [ "$T" -ge "$MAXT" ]; then
        P=255
      else
        # linear interpolation: P = MINP + (T-MINT)*(255-MINP)/(MAXT-MINT)
        P=$(( MINP + (T - MINT) * (255 - MINP) / (MAXT - MINT) ))
      fi
      echo "$P" > "$PWM"
      sleep 4
    done
  '';
in
{
  # Expose the motherboard fan headers (Nuvoton SuperIO).
  boot.kernelModules = [ "nct6775" ];

  # lm_sensors for manual inspection (`sensors`, `pwmconfig`).
  environment.systemPackages = [ pkgs.lm_sensors ];

  systemd.services.gpu-fan = {
    description = "Drive the CHA_FAN1 case fan from NVIDIA GPU temperature";
    wantedBy = [ "multi-user.target" ];
    after = [ "systemd-modules-load.service" ];
    serviceConfig = {
      Type = "simple";
      Restart = "always";
      RestartSec = 5;
      ExecStart = curve;
    };
  };
}
