# Standing medano watch + medano-independent sentry (lampadas only).
#
# Two timers, born from the 2026-11-15 self-recovery review:
#
#   */3min  medano-watch   If medano is reachable on its MAIN sshd (:22) ->
#                          healthy. If only the INITRD sshd (:2222, fleet
#                          key) answers -> medano sits at the ZFS prompt
#                          (out-of-band reboot: power cut, panic,
#                          generation-canary rollback - the nightly
#                          fleet-upgrade flow is not the only reboot source)
#                          -> feed grave+rpool passphrases, one ssh per pool,
#                          killall zfs, wait for the main sshd.
#                          Unreachable at all for >10min -> alert.
#
#   */5min  fleet-sentry   The medano-independent last-resort alert: every
#                          IRC path to #emile transits medano (DNAT/naraj or
#                          the fleet ssh tunnels), so a dead medano also
#                          kills alerting. This probes medano:22 and the
#                          public ircs port and EMAILS emile@emile.space via
#                          the (independent) mail host once per incident.
#
# The nightlies keep their own inline unlock in fleet-upgrade.sh; this
# standby watcher shares nothing with it and never reboots anything.

args1@{ hefe, pkgs, ... }:
args2@{ config, lib, ... }:

let
  syswatch = pkgs.callPackage hefe.ops.pkgs.syswatch { };

  mailHelper = pkgs.writeShellScript "fleet-mail" ''
    # fleet-mail <subject> <body...>  -  medano-independent alert channel
    STATE=/var/lib/fleet
    CONF=$STATE/msmtprc
    cat > "$CONF" <<EOF
    host mail.emile.space
    port 465
    tls on
    tls_starttls off
    tls_trust_file /etc/ssl/certs/ca-certificates.crt
    auth on
    user mail@emile.space
    from mail@emile.space
    passwordeval "cat ${config.age.secrets.mail_emile_space_password.path}"
EOF
    chmod 600 "$CONF"
    subject=$1; shift
    {
      echo "From: mail@emile.space"
      echo "To: emile@emile.space"
      echo "Subject: [lampadas fleet] $subject"
      echo ""
      echo "$@"
    } | ${pkgs.msmtp}/bin/msmtp -C "$CONF" emile@emile.space
  '';

  medanoWatch = pkgs.writeShellApplication {
    name = "medano-watch";
    runtimeInputs = with pkgs; [ openssh netcat-gnu coreutils gnugrep ];
    text = ''
      STATE=/var/lib/fleet
      mkdir -p "$STATE"
      INITRD_KH=$STATE/medano-initrd_known_hosts
      DOWN_SINCE=$STATE/medano-down-since
      MEDANO=95.217.35.60
      KEY=${config.age.secrets.fleet_unlock_medano_ed25519.path}
      PASS_FILE=${config.age.secrets.fleet_medano_zfs_passphrase.path}

      email() { ${mailHelper} "$@" || true; }
      say() {
        "${syswatch}/bin/syswatch" --tls --server irc.emile.space:6697 --channel '#emile' --say "lampadas: $1" || true
      }

      # ----------------------------- healthy? --------------------------------
      # main sshd listens on 22 AND 2222; the initrd one only on 2222.
      if nc -z -w 3 "$MEDANO" 22 2>/dev/null; then
        rm -f "$DOWN_SINCE"
        exit 0
      fi

      # ------------------------- initrd? then unlock --------------------------
      if ssh -o BatchMode=yes -o ConnectTimeout=8 \
             -o StrictHostKeyChecking=accept-new \
             -o UserKnownHostsFile="$INITRD_KH" \
             -i "$KEY" -p 2222 "root@$MEDANO" true 2>/dev/null; then
        echo "medano INITRD sshd alive - medano sits at the ZFS prompt, unlocking"
        PASS=$(cat "$PASS_FILE" 2>/dev/null)
        if [ -z "$PASS" ] || [ "$PASS" = REPLACE_ME ]; then
          email "medano locked in initrd; passphrase secret not filled in" \
                "fleet_medano_zfs_passphrase is empty/REPLACE_ME - manual unlock needed (ssh root@$MEDANO -p 2222)"
          exit 1
        fi
        # same trick as fleet-upgrade.sh: ONE ssh session PER POOL (glibc
        # getpass drains a shared pipe otherwise, 2026-11-11).
        ok=yes
        ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
            -o UserKnownHostsFile="$INITRD_KH" -i "$KEY" -p 2222 root@"$MEDANO" \
            'zpool import -a || true' 2>&1 || true
        for pool in grave rpool; do
          if ! printf '%s\n' "$PASS" | ssh -o ConnectTimeout=10 \
                 -o StrictHostKeyChecking=accept-new \
                 -o UserKnownHostsFile="$INITRD_KH" -i "$KEY" -p 2222 root@"$MEDANO" \
                 "zfs load-key $pool" 2>&1; then
            echo "zfs load-key $pool FAILED"
            ok=no
          fi
        done
        ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
            -o UserKnownHostsFile="$INITRD_KH" -i "$KEY" -p 2222 root@"$MEDANO" \
            'killall zfs || true' 2>&1 || true
        if [ "$ok" != yes ]; then
          email "medano ZFS unlock FAILED" \
                "key load failed for a pool - manual unlock needed (ssh root@$MEDANO -p 2222)"
          exit 1
        fi
        # wait for the real system
        for _ in $(seq 1 60); do
          nc -z -w 3 "$MEDANO" 22 2>/dev/null && { echo "medano booted"; rm -f "$DOWN_SINCE"; say "medano was locked in initrd - unlocked by the standby watcher, now booted"; exit 0; }
          sleep 10
        done
        email "medano unlock sent but main sshd never appeared" "manual check needed"
        exit 1
      fi

      # ---------------------------- fully dark --------------------------------
      now=$(date +%s)
      [ -f "$DOWN_SINCE" ] || echo "$now" > "$DOWN_SINCE"
      since=$(cat "$DOWN_SINCE")
      if [ $(( now - since )) -gt 600 ]; then
        if [ ! -f "$STATE/medano-dark-mailed" ]; then
          email "medano unreachable from lampadas for >10min" \
                "No sshd on :22 and no initrd sshd on :2222 since $(date -d "@$since" -Is). Every IRC alert path routes through medano, which is why you are getting an email instead."
          touch "$STATE/medano-dark-mailed"
        fi
      fi
      # cleared automatically again once medano is back (see the healthy branch)
      nc -z -w 3 "$MEDANO" 22 2>/dev/null && rm -f "$STATE/medano-dark-mailed" || true
    '';
  };

  sentry = pkgs.writeShellApplication {
    name = "fleet-sentry";
    runtimeInputs = with pkgs; [ netcat-gnu coreutils ];
    text = ''
      STATE=/var/lib/fleet
      mkdir -p "$STATE"
      fail_streak=0
      [ -f "$STATE/sentry-streak" ] && fail_streak=$(cat "$STATE/sentry-streak")

      # medano main sshd and the public ircs ingress (DNAT -> naraj -> irc
      # VM: also catches a dead irc VM when medano itself is up).
      if nc -z -w 3 95.217.35.60 22 2>/dev/null && nc -z -w 3 95.217.35.60 6697 2>/dev/null; then
        rm -f "$STATE/sentry-streak" "$STATE/sentry-mailed"
        exit 0
      fi

      fail_streak=$(( fail_streak + 1 ))
      echo "$fail_streak" > "$STATE/sentry-streak"
      echo "fleet-sentry: probe failed ($fail_streak consecutive)"

      if [ "$fail_streak" -ge 3 ] && [ ! -f "$STATE/sentry-mailed" ]; then
        ${mailHelper} "medano/irc ingress unreachable from lampadas" \
          "3 consecutive fleet-sentry failures (medano:22 and/or 95.217.35.60:6697). IRC alerting routes through medano, so this email is the alarm itself. medano-watch (*/3min) is on the unlock case if the initrd sshd is up." || true
        touch "$STATE/sentry-mailed"
      fi
    '';
  };
in
{
  systemd.services.medano-watch = {
    description = "standby medano initrd unlock (out-of-band reboots coverstory)";
    serviceConfig = {
      Type = "oneshot";
      StateDirectory = "fleet";
      TimeoutStartSec = "20min";
    };
    script = "exec ${medanoWatch}/bin/medano-watch";
  };

  systemd.timers.medano-watch = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "*:0/3:00";
      Persistent = true;
    };
  };

  systemd.services.fleet-sentry = {
    description = "medano-independent alert probe; emails when the IRC path is dark";
    serviceConfig = {
      Type = "oneshot";
      StateDirectory = "fleet";
      TimeoutStartSec = "2min";
    };
    script = "exec ${sentry}/bin/fleet-sentry";
  };

  systemd.timers.fleet-sentry = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "*:0/5:00";
      Persistent = true;
    };
  };
}
