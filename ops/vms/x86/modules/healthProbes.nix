# Lightweight health-probe runner.
#
# Each VM (and the medano host) can declare a list of HTTP(S) probes that a
# systemd timer runs every minute. Results are written as prometheus textfile
# metrics under /var/lib/node-exporter/textfile, picked up by the node_exporter
# textfile collector that vm-base already runs.
#
# Usage:
#   services.healthProbes = {
#     enable = true;
#     probes = [
#       { name = "self"; url = "http://127.0.0.1:9091/"; }
#       { name = "public"; url = "https://md.medano.emile.space/"; expectedStatus = 200; }
#     ];
#   };
#
# Metrics exposed (one set per probe):
#   health_probe_up{name,url} 0|1
#   health_probe_http_code{name,url} <code>
#   health_probe_duration_seconds{name,url} <seconds>
#   health_probe_last_run_timestamp_seconds <unix>

{ config, lib, pkgs, ... }:

let
  cfg = config.services.healthProbes;

  probeOpts = { name, ... }: {
    options = {
      name = lib.mkOption {
        type = lib.types.str;
        description = "Probe identifier, used as the metric label.";
      };
      url = lib.mkOption {
        type = lib.types.str;
        description = "HTTP(S) URL to probe.";
      };
      expectedStatus = lib.mkOption {
        type = lib.types.int;
        default = 200;
        description = "HTTP code the probe should return for health_probe_up=1.";
      };
      timeout = lib.mkOption {
        type = lib.types.int;
        default = 5;
        description = "curl --max-time in seconds.";
      };
    };
  };

  # The textfile directory used by node-exporter to pick up extra metrics.
  textfileDir = "/var/lib/node-exporter/textfile";

  runProbesScript = pkgs.writeShellScript "run-health-probes" ''
    set -u
    mkdir -p ${textfileDir}
    OUT=${textfileDir}/health_probes.prom
    TMP=$(${pkgs.coreutils}/bin/mktemp ${textfileDir}/.health_probes.XXXXXX)
    chmod 0644 $TMP

    echo "# HELP health_probe_up 1 if the probe returned the expected HTTP code, 0 otherwise" >> $TMP
    echo "# TYPE health_probe_up gauge" >> $TMP
    echo "# HELP health_probe_http_code Last HTTP code returned by the probe (0 on transport failure)" >> $TMP
    echo "# TYPE health_probe_http_code gauge" >> $TMP
    echo "# HELP health_probe_duration_seconds Wall-clock time for the probe to complete" >> $TMP
    echo "# TYPE health_probe_duration_seconds gauge" >> $TMP

    ${lib.concatMapStringsSep "\n" (p: ''
      {
        name="${p.name}"
        url="${p.url}"
        expected=${toString p.expectedStatus}
        out=$(${pkgs.curl}/bin/curl \
          --max-time ${toString p.timeout} \
          --silent --show-error --output /dev/null \
          --write-out "%{http_code} %{time_total}" \
          -k \
          "$url" 2>/dev/null || echo "0 0")
        code=$(echo "$out" | ${pkgs.gawk}/bin/awk '{print $1}')
        dur=$(echo "$out" | ${pkgs.gawk}/bin/awk '{print $2}')
        if [ "$code" = "$expected" ]; then up=1; else up=0; fi
        printf 'health_probe_up{name="%s",url="%s"} %d\n' "$name" "$url" "$up" >> $TMP
        printf 'health_probe_http_code{name="%s",url="%s"} %d\n' "$name" "$url" "$code" >> $TMP
        printf 'health_probe_duration_seconds{name="%s",url="%s"} %s\n' "$name" "$url" "$dur" >> $TMP
      }
    '') cfg.probes}

    echo "# HELP health_probe_last_run_timestamp_seconds Unix timestamp of the last probe run" >> $TMP
    echo "# TYPE health_probe_last_run_timestamp_seconds gauge" >> $TMP
    echo "health_probe_last_run_timestamp_seconds $(${pkgs.coreutils}/bin/date +%s)" >> $TMP

    mv $TMP $OUT
  '';
in
{
  options.services.healthProbes = {
    enable = lib.mkOption {
      type = lib.types.bool;
      default = (cfg.probes or []) != [];
      defaultText = lib.literalExpression "probes != []";
      description = "Run periodic HTTP probes and export metrics via node-exporter textfile.";
    };

    probes = lib.mkOption {
      type = lib.types.listOf (lib.types.submodule probeOpts);
      default = [];
      description = "List of HTTP(S) probes.";
    };

    interval = lib.mkOption {
      type = lib.types.str;
      default = "1min";
      description = "systemd OnUnitActiveSec interval between probe runs.";
    };
  };

  config = lib.mkIf cfg.enable {
    # vm-base already enables node-exporter; point it at our textfile dir.
    services.prometheus.exporters.node = {
      enabledCollectors = lib.mkAfter [ "textfile" ];
      extraFlags = lib.mkAfter [ "--collector.textfile.directory=${textfileDir}" ];
    };

    systemd.tmpfiles.rules = [
      "d ${textfileDir} 0755 root root - -"
    ];

    systemd.services.health-probes = {
      description = "Run configured HTTP health probes";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = "${runProbesScript}";
      };
    };

    systemd.timers.health-probes = {
      description = "Periodic health-probe runner";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "30s";
        OnUnitActiveSec = cfg.interval;
        AccuracySec = "5s";
      };
    };
  };
}
