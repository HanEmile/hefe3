#!/usr/bin/env bash
# fleet-upgrade - nightly fleet auto-upgrade, runs on lampadas.
# See ops/modules/fleet-upgrade.nix for the big picture.
#
# Environment (set by the systemd unit):
#   FLEET_CHECKOUT      throwaway hefe3 checkout (created if missing)
#   FLEET_REPO_URL      clone source
#   FLEET_MEDANO        medano's public IP
#   FLEET_VMS           space-separated VM names
#   FLEET_SYSWATCH      syswatch binary (IRC)
#   FLEET_ZFS_PASS_FILE medano ZFS passphrase (age secret)
#
# Never exits non-zero: every failure lands in results.json / the 08:00
# report instead of leaving the fleet half-upgraded silently.

set -uo pipefail

# root@lampadas has no curated known_hosts; accept on first contact.
export GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=accept-new"

STATE=/var/lib/fleet
RESULTS=$STATE/results.json
LOG=$STATE/last-run.log
STARTED=$(date -Is)

mkdir -p "$STATE"
exec > >(tee -a "$LOG") 2>&1
echo "===== fleet-upgrade $STARTED ====="

# IRC plumbing: the public ircs ingress (medano DNAT -> naraj TLS -> Ergo)
# proved flaky from lampadas for fresh TLS connections (2026-11-02: openssl
# connects fine, Go's tls.Dial+registration times out; unresolved). Route
# through an ssh tunnel to Ergo's localhost plaintext instead - ssh to
# medano is something this script needs anyway and can rely on.
IRC_TUNNEL=""
irc_setup() {
  if [ -n "$IRC_TUNNEL" ]; then
    return 0
  fi
  ssh -o BatchMode=yes -o ConnectTimeout=10 -N -L 16667:192.168.75.23:6667 medano-root >/dev/null 2>&1 &
  IRC_TUNNEL=$!
  sleep 3
}
irc_cleanup() {
  [ -n "$IRC_TUNNEL" ] && kill "$IRC_TUNNEL" 2>/dev/null
  return 0 # a failed test must never set -e-kill the caller (finish/EXIT-trap)
}

say() {
  # best-effort IRC line, a few retries
  local _try
  for _try in 1 2 3; do
    irc_setup
    "$FLEET_SYSWATCH" --server 127.0.0.1:16667 --channel '#emile' --say "$1" && return 0
    kill "$IRC_TUNNEL" 2>/dev/null; IRC_TUNNEL=""
    sleep 10
  done
  return 0
}

# ---------------------------------------------------------------- helpers --

# deploy_one <system>: evaluate drv locally, ship, build+switch on target.
deploy_one() {
  local name=$1
  local rlink="$STATE/result-$name"
  echo "-- deploy $name"
  if ! nix-build "$FLEET_CHECKOUT" -A "ops.nixos.$name.deploy" -o "$rlink" >>"$LOG.nix" 2>&1; then
    echo "   nix-build FAILED"
    return 1
  fi
  if ! "$rlink/bin/deploy" >>"$LOG" 2>&1; then
    echo "   deploy FAILED"
    return 1
  fi
  echo "   ok"
  return 0
}

# kernel_changed <ssh-target>: true when booted kernel != switched generation
kernel_changed() {
  local target=$1
  ssh -o ConnectTimeout=10 -o BatchMode=yes "$target" \
    'test "$(readlink /run/booted-system/kernel 2>/dev/null)" != \
          "$(readlink /run/current-system/kernel 2>/dev/null)"' 2>/dev/null
}

wait_ssh() { # target, timeout_sec
  local target=$1 deadline=$(( $(date +%s) + $2 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    ssh -o ConnectTimeout=5 -o BatchMode=yes "$target" true 2>/dev/null && return 0
    sleep 10
  done
  return 1
}

wait_port() { # host port timeout_sec
  local deadline=$(( $(date +%s) + $3 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    nc -z -w 3 "$1" "$2" 2>/dev/null && return 0
    sleep 10
  done
  return 1
}

declare -A DEPLOYED REBOOTED BACKUP ERR
record() { DEPLOYED[$1]=$2; REBOOTED[$1]=${3:-no}; BACKUP[$1]=${4:-skip}; ERR[$1]=${5:-}; }

write_results() { # finished_iso
  {
    echo -n '{"pin":'"$(cat "$STATE/pin.txt" 2>/dev/null || echo '"unknown"')"
    echo ',"started_at":"'"$STARTED"'"'
    echo ',"finished_at":"'"$1"'"'
    echo -n ',"hosts":['
    local first=1 name
    # NOTE: "${!arr[@]:-}" on an assoc prints VALUES, not keys (bash quirk)
    # - the earlier "empty-trap" fix with a :- fallback silently broke every
    # populated write. "declare -A" above always exists, so plain expansion
    # is nounset-safe since bash 4.4.
    for name in $(printf '%s\n' "${!DEPLOYED[@]}" | sort); do
      [ $first -eq 0 ] && echo -n ,
      first=0
      jq -cn --arg name "$name" --arg dep "${DEPLOYED[$name]}" \
             --arg reb "${REBOOTED[$name]:-no}" --arg up "${BACKUP[$name]:-skip}" \
             --arg err "${ERR[$name]:-}" \
        '{name:$name,deployed:$dep,rebooted:$reb,back_up:$up,error:$err}'
    done
    echo ']}'
  } > "$RESULTS.tmp"
  mv "$RESULTS.tmp" "$RESULTS"
}

finish() {
  irc_cleanup
  write_results "$(date -Is)"
  # ship to status-board on medano (best-effort; medano may be mid-reboot,
  # fleet-ship.timer -> the 08:00 report also retries via its own ssh)
  scp -o ConnectTimeout=10 -o BatchMode=yes "$RESULTS" \
    "root@$FLEET_MEDANO:/var/lib/status-board/fleet.json" 2>/dev/null || true
}
trap finish EXIT

# ------------------------------------------------------------- 1. pin bump --

if [ ! -d "$FLEET_CHECKOUT/.git" ]; then
  echo "cloning $FLEET_REPO_URL -> $FLEET_CHECKOUT"
  rm -rf "$FLEET_CHECKOUT"
  git clone --quiet "$FLEET_REPO_URL" "$FLEET_CHECKOUT" || {
    record lampadas fail no fail "git clone failed"
    rm -f "$STATE/pin.txt"
    say "fleet-upgrade: git clone FAILED - giving up for tonight"
    exit 0
  }
fi

cd "$FLEET_CHECKOUT"
# This checkout is a throwaway - dirty worktrees AND stale untracked cruft
# (rsync leftovers, half-built binaries) must never kill the run, so clean
# hard before switching (otherwise checkout -B aborts, observed 2026-11-04).
git fetch -q origin main
git reset -q --hard HEAD
git clean -fdx
git checkout -q -B fleet-auto origin/main

NPINS="npins --directory third_party/npins"
if $NPINS show nixos-26.05 >/dev/null 2>&1; then
  $NPINS update nixos-26.05 >>"$LOG" 2>&1 || {
    say "fleet-upgrade: npins update FAILED - keeping yesterday's pin"
  }
fi
# npins show prints yaml-ish text ("url: https://...unquoted..."), not json;
# grep -o failing finds nothing -> pipefail + set -e killed the whole run
# here on 2026-11-03. || true keeps a bad parse survivable.
PIN_RELEASE=$($NPINS show nixos-26.05 2>/dev/null \
  | grep -oE 'nixos-26\.05\.[0-9a-z.]+' | head -1 | sed 's/^nixos-//' || true)
echo "\"${PIN_RELEASE:-unknown}\"" > "$STATE/pin.txt"
git add third_party/npins/sources.json
git -c user.name=fleet-upgrade -c user.email=fleet@emile.space \
  commit -q -m "fleet-auto: bump nixos-26.05 -> ${PIN_RELEASE:-unknown}" || true
echo "pin: ${PIN_RELEASE:-unknown}"

# ------------------------------------------------------- 2. deploy the VMs --

# 4 at a time, like deploy_all.sh - builds run ON the targets
pids=()
running=0
for vm in $FLEET_VMS; do
  (
    if deploy_one "$vm"; then
      # post-switch canary: if ssh is already dead, the in-guest
      # generation-canary will roll back; we stop poking it here.
      if wait_ssh "$vm" 300; then
        echo "ok" > "$STATE/deploy-$vm.status"
      else
        echo "canary-fail" > "$STATE/deploy-$vm.status"
      fi
    else
      echo "fail" > "$STATE/deploy-$vm.status"
    fi
  ) &
  pids+=($!)
  running=$((running + 1))
  if [ "$running" -ge 4 ]; then
    wait -n || true
    running=$((running - 1))
  fi
done
wait || true

for vm in $FLEET_VMS; do
  record "$vm" "$(cat "$STATE/deploy-$vm.status" 2>/dev/null || echo fail)"
done

# ---------------------------------------------------- 3. deploy medano -----

if deploy_one medano; then
  record medano ok
else
  record medano fail no skip "deploy failed"
fi

# ----------------------------------------------------- 4. reboot medano ----

medano_rebooted=no
if [ "${DEPLOYED[medano]}" = ok ] && kernel_changed medano; then
  echo "-- rebooting medano (kernel changed)"
  EXPECTED_VMS=$(ssh -o BatchMode=yes medano-root 'virsh list --name | grep -c .' 2>/dev/null || echo "?")
  ssh -o BatchMode=yes medano-root 'systemctl reboot' 2>/dev/null || true
  medano_rebooted=yes
  REBOOTED[medano]=yes

  echo "   waiting for the initrd sshd on :2222"
  sleep 60
  if wait_port "$FLEET_MEDANO" 2222 600; then
    PASS=$(cat "$FLEET_ZFS_PASS_FILE" 2>/dev/null)
    if [ -z "$PASS" ] || [ "$PASS" = "REPLACE_ME" ]; then
      ERR[medano]="unlock skipped: medano_zfs_passphrase.age not filled in"
      BACKUP[medano]=fail
      say "fleet-upgrade: medano rebooted but I CANNOT UNLOCK ZFS (passphrase secret not filled in - see secrets.nix)"
    else
      # One ssh session PER POOL: without a tty, `zfs load-key` reads stdin
      # via glibc getpass(), which drains the whole pipe in a single read().
      # Piping "PASS\nPASS\n" into one session fed BOTH lines to the first
      # prompt (grave) and left the second prompt (rpool) reading EOF ->
      # "Key load error: encryption failure" -> medano sat locked in the
      # initrd all night (2026-11-11).
      # 'import' may rc!=0 when pools are already up; killall has no target
      # if the console prompt already exited - both are fine.
      unlock_ok=yes
      ssh -o ConnectTimeout=10 medano-initrd 'zpool import -a || true' >>"$LOG" 2>&1 || true
      for pool in grave rpool; do
        if ! printf '%s\n' "$PASS" | ssh -o ConnectTimeout=10 medano-initrd \
             "zfs load-key $pool" >>"$LOG" 2>&1; then
          echo "   zfs load-key $pool FAILED"
          unlock_ok=no
        fi
      done
      # the console-side 'zfs load-key -a' prompt blocks stage-1; stage-1's
      # retry loop then notices the keys and proceeds with boot.
      ssh -o ConnectTimeout=10 medano-initrd 'killall zfs || true' >>"$LOG" 2>&1 || true
      if [ "$unlock_ok" = yes ]; then
        echo "   unlock command delivered"
      else
        ERR[medano]="zfs load-key failed during initrd unlock (see $LOG)"
        say "fleet-upgrade: medano rebooted but ZFS LOAD-KEY FAILED - MANUAL UNLOCK NEEDED"
      fi
    fi
  else
    ERR[medano]="initrd sshd never came up after reboot"
    BACKUP[medano]=fail
    say "fleet-upgrade: medano rebooted but its initrd sshd never appeared - MANUAL UNLOCK NEEDED"
  fi

  if wait_ssh medano-root 900; then
    BACKUP[medano]=ok
    if [ "$EXPECTED_VMS" != "?" ]; then
      echo "   waiting for $EXPECTED_VMS VMs to autostart"
      deadline=$(( $(date +%s) + 900 ))
      while [ "$(date +%s)" -lt "$deadline" ]; do
        # failing ssh prints "0" (from the grep) plus "0" (error fallback):
        # keep only the first line to avoid "0\n0: integer expected"
        got=$(ssh -o BatchMode=yes medano-root 'virsh list --name | grep -c .' 2>/dev/null || echo 0)
        got=${got%%$'\n'*}
        [ "$got" -ge "$EXPECTED_VMS" ] && break
        sleep 15
      done
      echo "   VMs back: $got/$EXPECTED_VMS"
    fi
    # the host reboot already cycled every VM into its switched generation
    for vm in $FLEET_VMS; do
      [ "${DEPLOYED[$vm]:-fail}" = ok ] && REBOOTED[$vm]=yes
    done
  else
    BACKUP[medano]=fail
    ERR[medano]="${ERR[medano]:-}; never ssh-able after unlock"
    say "fleet-upgrade: medano IS STILL DOWN after unlock attempt"
  fi
fi

# ------------------------------------------------- 5. reboot changed VMs ---

if [ "${BACKUP[medano]:-skip}" = ok ] || [ "$medano_rebooted" = no ]; then
  for vm in $FLEET_VMS; do
    [ "${DEPLOYED[$vm]:-fail}" != ok ] && continue
    [ "${REBOOTED[$vm]:-no}" = yes ] && continue
    if kernel_changed "$vm"; then
      echo "-- virsh reboot $vm (kernel changed)"
      ssh -o BatchMode=yes medano-root "virsh reboot $vm" 2>/dev/null || true
      if wait_ssh "$vm" 600; then
        REBOOTED[$vm]=yes
        BACKUP[$vm]=ok
      else
        REBOOTED[$vm]=fail
        ERR[$vm]="never ssh-able after reboot"
        say "fleet-upgrade: $vm did not come back after its reboot"
      fi
    fi
  done
fi

echo "===== fleet-upgrade done $(date -Is) ====="
