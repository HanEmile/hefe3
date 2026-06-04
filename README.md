# hefe3

NixOS monorepo for the emile.space fleet. One Hetzner host (`medano`)
hypervises ~23 libvirt VMs; two more bare-metal machines (`lampadas`,
the `mail` host) and the Hetzner Storagebox sit outside. `caladan`
(this Mac) is the dev/deploy box: every `nix-build` and `deploy`
in here originates from caladan and uses medano as the remote x86_64
builder.

## Hosts

| Host        | Role                                | Public v4       | Tailscale v4      | Config                                          |
|-------------|-------------------------------------|-----------------|-------------------|-------------------------------------------------|
| caladan     | dev/deploy box (macOS, aarch64)     | - | (tailnet)         | `ops/machines/aarch64/caladan/`                 |
| medano      | Hetzner hypervisor                  | 95.217.35.60    | (tailnet)         | `ops/machines/x86/medano/`                      |
| lampadas    | secondary bare-metal (off-site)     | - | 100.87.209.97     | `ops/machines/x86/lampadas/`                    |
| mail        | mail host                           | - | (tailnet)         | `ops/machines/x86/mail/`                        |
| naraj       | TLS-terminating reverse proxy VM    | (DNAT via med.) | (tailnet)         | `ops/vms/x86/naraj/`                            |
| auth        | Authelia SSO VM                     | - | (tailnet)         | `ops/vms/x86/auth/`                             |
| md          | HedgeDoc                            | - | (tailnet)         | `ops/vms/x86/md/`                               |
| git         | Forgejo / git hosting               | - | (tailnet)         | `ops/vms/x86/git/`                              |
| data        | SFTPGo                              | - | 100.104.120.80    | `ops/vms/x86/data/`                             |
| miki        | wiki                                | - | (tailnet)         | `ops/vms/x86/miki/`                             |
| photo       | Immich                              | - | (tailnet)         | `ops/vms/x86/photo/`                            |
| social      | GoToSocial                          | - | (tailnet)         | `ops/vms/x86/social/`                           |
| rss         | Miniflux (tailscale-only)           | - | 100.70.149.84     | `ops/vms/x86/rss/`                              |
| tmp         | scratch                             | - | (tailnet)         | `ops/vms/x86/tmp/`                              |
| amalthea    | Immich frontend / sync              | - | (tailnet)         | `ops/vms/x86/amalthea/`                         |
| late        | `late.sh` paste service             | - | (tailnet)         | `ops/vms/x86/late/`                             |
| demo01      | reference / template VM             | - | (tailnet)         | `ops/vms/x86/demo01/`                           |
| sb1/sb2/sb3 | standby Linux VMs                   | - | (tailnet)         | `ops/vms/x86/sb{1,2,3}/`                        |
| minecraft   | Minecraft (UDP 25565 forwarded)     | (DNAT via med.) | (tailnet)         | `ops/vms/x86/minecraft/`                        |
| factorio    | Factorio (UDP 34197 forwarded)      | (DNAT via med.) | (tailnet)         | `ops/vms/x86/factorio/`                         |
| r2wars      | r2wars site                         | - | (tailnet)         | `ops/vms/x86/r2wars/`                           |
| rou         | VPN exit (private bridge)           | 192.168.33.2    | (tailnet)         | `ops/vms/x86/rou/`                              |
| arr         | *arr-stack on `rou` private bridge  | 192.168.33.3    | (tailnet)         | `ops/vms/x86/arr/`                              |

Tailnet domain is `pinto-pike.ts.net`. Full IPAM in
`ops/ipam/default.nix` (default bridge, `private`, `rou`, and
`tailscale` namespaces).

## Repo layout

```
hefe3/
  default.nix                  readTree entry; allowlists users/ paths per host
  deploy_all.sh                sequential fleet deploy (best-effort)
  deploy.sh, install.sh        legacy single-host scripts
  npins/                       npins-managed third-party pins (sources.nix)
  third_party/                 thin wrapper around npins for use in readTree
  ops/
    ipam/                      SINGLE SOURCE OF TRUTH for VM IPs+MACs+ports
    acl/                       per-host users/groups, derived from //users/*/keys
    nixos.nix                  build + deployScriptForOpts machinery (see below)
    darwin.nix                 caladan (nix-darwin)
    lib/                       mkVmImage.nix etc.
    pkgs/                      local nix packages (makhor, ...)
    shells/                    nix-shell envs
    modules/                   shared NixOS modules (late-sh, ...)
    machines/aarch64/caladan/  caladan host config
    machines/x86/medano/       hypervisor (networking.nix has natFlows)
    machines/x86/lampadas/     off-site bare-metal
    machines/x86/mail/         mail host
    vms/x86/<vm>/              per-VM configs (default.nix + libvirt.nix)
    vms/x86/vm-base.nix        shared VM NixOS config (network from IPAM, ssh, tailscale, node-exporter)
    vms/x86/libvirt-base.nix   shared libvirt domain template; restart = null
    vms/x86/hardware-image.nix generic hw-config for image-built VMs
    vms/x86/modules/
      backups.nix              vmBackups option -> restic to storagebox
      healthProbes.nix         systemd timer -> prometheus textfile metrics
      tailscale-cert-renew.nix `tailscale cert` + nginx reload on a weekly timer
    secrets/                   agenix .age files + secrets.nix + README.md
  tools/
    status-board/              Go dashboard (main.go, forcegraph.go, natviz.go)
  users/
    hanemile/                  dotfiles, emacs, emile_space site, keys/, projects/, reversing/
```

Companion docs (do not duplicate here):
`MIGRATION.md`, `CORRINO-DECOMMISSION.md`, `NAMECHEAP-EMILE-SPACE.md`.

## Deploy commands

All deploys are run from caladan in the `hefe3/` checkout. Builds are
forced to `x86_64-linux` and shipped to medano via remote-builder
(see `/etc/nix/machines` on caladan).

```sh
# Per-VM config switch (most common)
nix-build -A ops.nixos.<vm>.deploy && ./result/bin/deploy

# Host switch (medano, caladan, lampadas, mail)
nix-build -A ops.nixos.<host>.deploy && ./result/bin/deploy

# First-time VM bootstrap: builds qcow2, uploads to /keep/pools/vmpool/<vm>.qcow2
nix-build -A ops.nixos.<vm>.deploy_image && ./result/bin/deploy-image

# Personal static site
nix-build -A users.hanemile.emile_space.deploy && ./result/bin/deploy

# Status-board binary (consumed by the medano module)
nix-build -A tools.status-board.package

# Whole fleet, sequential
./deploy_all.sh
```

Gotchas:

- `NIXPKGS_ALLOW_UNSUPPORTED_SYSTEM=1` is set inside `deploy_image`
  because we are building an x86_64-linux qcow2 from a darwin/aarch64
  host. This only works because `/etc/nix/machines` on caladan points
  at `ssh://medano.emile.space x86_64-linux ... kvm` and medano does
  the real work.
- The build helper passes `-j 0` (no local builds). Do NOT change to
  `--max-jobs auto`: darwin will then try to compile Linux-only deps
  (cifs-utils, keyutils) and fail.
- `nix copy --to ssh-ng://...?compress=true --no-check-sigs` is the
  transport. Drop `--no-check-sigs` and pushes from caladan break
  (the store paths aren't signed by a key medano trusts).
- `deploy` re-runs the build, then `nix copy`, then sets the system
  profile, then `switch-to-configuration switch`. `deploy_ts` is the
  same but uses the `<host>.pinto-pike.ts.net` MagicDNS name.

## Bootstrapping a new VM

See `ops/vms/x86/README.md` for the long version. Short version:

1. **IPAM** - add to `ops/ipam/default.nix` under `default` (or
   `private` for VPN-routed VMs). MAC = `02:` + md5sum prefix:

   ```sh
   echo "<vmname>" | md5sum | sed 's/^\(..\)\(..\)\(..\)\(..\)\(..\).*$/02:\1:\2:\3:\4:\5/'
   ```

2. **ACL** - add `<vmname> = withDefault { };` in `ops/acl/default.nix`.

3. **readTree allow-list** - if the VM imports anything from `//users`
   (which `vm-base.nix` does via `acl`), add
   `[ "ops" "vms" "x86" "<vmname>" ]` to the `exceptions` list in the
   root `default.nix`.

4. **VM directory** - `ops/vms/x86/<vmname>/{default.nix,libvirt.nix}`,
   copying from `demo01/` as the minimal template. (No
   `hardware-configuration.nix`; image-built VMs import
   `../hardware-image.nix`.)

5. **Wire into medano** - add `(vm "<vmname>")` to
   `ops/machines/x86/medano/default.nix`.

6. **First boot** - `nix-build -A ops.nixos.<vmname>.deploy_image &&
   ./result/bin/deploy-image`. This refuses to overwrite a running
   VM's disk; libvirt will define + start the domain on the next
   medano deploy.

7. **Host key into secrets** - after first boot, grab
   `/etc/ssh/ssh_host_ed25519_key.pub` from the VM, add it to
   `ops/secrets/secrets.nix`, then `cd ops/secrets && ragenix -r`
   so every age secret the VM needs is re-encrypted to its key.

8. **Tailscale (manual)** - `ssh -J medano root@<vm-ip>
   'tailscale up --ssh'`; follow the printed URL.

9. **Subsequent updates** - `nix-build -A ops.nixos.<vmname>.deploy
   && ./result/bin/deploy`.

## Secrets (agenix / ragenix)

Recipients live in `ops/secrets/secrets.nix`. Files end in `.age`.

```sh
# Edit a secret
cd ops/secrets
ragenix --editor hx --edit lampadas_pinto_pike_ts_net_key.age

# Rekey every secret after editing secrets.nix (added a recipient,
# rotated a host key, added a new VM, ...)
ragenix -r
```

- Use the pre-installed `ragenix`. The fallback
  `nix run git+https://github.com/ryantm/agenix` is blocked by the
  sub-agent sandbox.
- Filename / attr name normalisation: `.age` filenames use `.`,
  but the `age.secrets` attr name can not contain `.`, so we
  use `_`. Example: file
  `lampadas_pinto_pike_ts_net_key.age` decrypts to
  `/run/agenix/lampadas.pinto-pike.ts.net.key` in some modules.
  Read the relevant VM's `default.nix` to confirm.
- When to rekey: every time `secrets.nix` changes recipients
  (new VM, new admin key, dropped host).

## Networking topology

Public DNS `*.emile.space` resolves to `95.217.35.60` (medano,
Hetzner). Internally:

```
                 internet
                    |
                eno1 (95.217.35.60)
                    |
                  medano
            +-------+-------+-----------+
            |               |           |
        virbr0           virbr1       virbr2
    192.168.75.0/24   192.168.33.0/24  192.168.34.0/24
       (default)        (private,        (rou)
                         wireguard)
            |               |           |
   naraj, auth, md,    rou, arr        rou
   git, data, miki,
   photo, social, rss,
   tmp, amalthea, late,
   demo01, sb{1,2,3},
   minecraft, factorio,
   r2wars
```

- All public 80/443 → `naraj` (192.168.75.2) via PREROUTING DNAT
  declared in `ops/machines/x86/medano/networking.nix` (`natFlows`
  list). Same module generates `/etc/nat-flows.json` consumed by
  the status-board NAT viz.
- naraj terminates TLS for every public `*.emile.space` hostname
  and reverse-proxies onward to the appropriate VM.
- Hairpin NAT: VMs reaching the pubv4 from inside the bridge get
  DNAT'd back to naraj (so internal clients see the same path as
  external).
- SNAT-on-return on `eno1` rewrites the source to `95.217.35.60`
  so external clients see medano's address.
- Tailscale is enabled on most VMs via `vm-base.nix`. `rss`
  serves HTTPS via `tailscale serve`. `arr` (and others that run
  their own nginx) use `tailscaleCertRenew.hostnames` for
  90-day-renewing certs landing in `/var/lib/tailscale-certs/`.

## SSO

- IdP: Authelia on the `auth` VM. Canonical URL
  `https://sso.emile.space`. Cookie domain `emile.space`.
- Policy: `one_factor` (no 2FA mandated; WebAuthn + TOTP are
  configured for opt-in).
- OIDC clients live as separate files under
  `ops/vms/x86/auth/oidc_clients/`:
  `amalthea.nix`, `gotosocial.nix`, `hedgedoc.nix`, `immich.nix`,
  `miniflux.nix`, `sftpgo.nix`.
- Internal services that lack OIDC (status-board) are gated via
  nginx `auth_request` forward-auth on naraj.
- **Issuer rename gotcha**: if you change the Authelia issuer URL,
  every OIDC client breaks until the relevant `*_oidc_client_secret`
  / env-file `.age` is re-encrypted with the new issuer values.
  Use the `EDITOR=` pattern with ragenix and rekey on every affected
  recipient.

## Backups

Per-VM restic backups via the `vmBackups` module
(`ops/vms/x86/modules/backups.nix`).

```nix
imports = [
  (import ../modules/backups.nix { inherit hefe; })
];

vmBackups = {
  paths = [ "/var/lib/hedgedoc" ];
  # optional:
  # excludePatterns = [ "*.tmp" ];
  # onCalendar = "*-*-* 03:17:00";  # default randomised offset 03:17
  # backupPrepareCommand = ''
  #   pg_dump -U postgres mydb > /var/lib/mydb.dump
  # '';
};
```

- Repo path: `/mnt/storagebox-bx11/backup/<hostname>` (auto from
  `config.networking.hostName`).
- Storage: Hetzner Storagebox mounted via CIFS at
  `/mnt/storagebox-bx11`, autofs with
  `noauto,x-systemd.automount` (idle-unmounts).
- Required secrets:
  `storagebox_bx11_restic_password.age` and
  `storagebox_bx11_connection_config.age` - both keyed to every
  VM that backs up.
- Default retention: `--keep-daily 7 --keep-weekly 5
  --keep-monthly 12 --keep-yearly 15`.
- Use `backupPrepareCommand` for stateful dumps (postgres on rss,
  miniflux) - runs before the restic snapshot.

## TLS / certs

Three patterns:

1. **Public `*.emile.space` hosts** - ACME http-01 on `naraj`'s
   nginx. naraj is the only thing that gets public 80/443 traffic.
2. **`tailscale serve`** - used by `rss` for Miniflux. tailscaled
   manages the cert; nothing for us to maintain.
3. **Self-hosted nginx behind tailscale** - used by `arr`.
   `tailscaleCertRenew.hostnames = [ "arr.pinto-pike.ts.net" ];`.
   Module writes certs to `/var/lib/tailscale-certs/<host>.{crt,key}`
   (mode 0640, group `nginx`) and reloads nginx on renewal.

## Status board

- Source: `tools/status-board/{default.nix,main.go,forcegraph.go,natviz.go,go.mod}`.
- Runs on medano, bound to `127.0.0.1:8090`; medano nginx serves it
  publicly at `https://status.medano.emile.space/` gated by
  Authelia `auth_request` forward-auth (also reachable from
  `192.168.75.1:8090` for in-VM debugging).
- Scrapes each VM's `:9100/metrics` (node-exporter + textfile from
  the `healthProbes` module) over tailscale.
- Calls `virsh list --all` and `virsh dominfo <name>` on medano
  for libvirt state.
- Walks `/mnt/storagebox-bx11/backup/<vm>/snapshots/` for restic
  freshness (stat-only; no repo password needed).
- Reads nix-generated drops:
 - `/etc/status-board-graph.json` (force-graph topology)
 - `/etc/nat-flows.json` (natFlows from medano networking.nix)
- Env vars:
 - `STATUS_BOARD_INVENTORY` - JSON `[{name, ip, bridge}, ...]`
 - `STATUS_BOARD_LISTEN` (default `127.0.0.1:8090`)
 - `STATUS_BOARD_STORAGEBOX` (default `/mnt/storagebox-bx11/backup`)

## Common gotchas

- `ops/vms/x86/libvirt-base.nix` sets `restart = null` on every
  domain. This is load-bearing: without it, editing a single VM
  would churn all 22 domains on every medano switch.
- `ops/vms/x86/arr/private.nix` is **gitignored** (NFS mount
  details + container env). Do NOT `git add -f` it. There is a
  matching `private.nix.age` that holds the real secret content;
  see `arr/age-{encrypt,decrypt}.sh` for the wrapper.
- `nix copy --to ssh-ng://...?compress=true --no-check-sigs` - 
  removing `--no-check-sigs` will break every deploy from caladan
  (store paths aren't signed by anyone medano trusts).
- `-j 0` in the build helper (`ops/nixos.nix`): do NOT change to
  `--max-jobs auto`. darwin will start compiling linux-only
  derivations (cifs-utils, keyutils, ...) and fail.
- Autofs CIFS to storagebox: `du -shx` on a restic data dir wedges
  under concurrent load. status-board wraps the directory walk in
  a 1.5s context to keep the dashboard responsive.
- Tailscale auth is **not** automated on first boot. After
  `deploy-image`, you must
  `ssh -J medano root@<vm-ip> 'tailscale up --ssh'` and open the
  printed URL.
- Sub-agent sandbox: `nix run git+https://github.com/ryantm/agenix`
  is blocked. Always use the pre-installed `ragenix`.
- Image-built VMs have **no** `hardware-configuration.nix`; they
  import `../hardware-image.nix` which describes the layout
  produced by `ops/lib/mkVmImage.nix` (MBR, ext4 root labeled
  `nixos`, grub on `/dev/sda`).
- `deploy-image` refuses to overwrite a running VM's disk
  (libvirt holds the file open; silent swap = corrupted state).
  `virsh shutdown <vm>` first, redeploy, `virsh start <vm>`.
- `MIGRATION.md`, `CORRINO-DECOMMISSION.md`,
  `NAMECHEAP-EMILE-SPACE.md` exist for
  history / one-shot procedures - read them, don't duplicate
  here.
- caladan's `/etc/nix/machines` contains exactly one builder line
  pointing at medano. If that breaks (medano down, key rotated)
  every deploy stalls. Check it before debugging deeper.

## Where things live (cheat sheet)

| I want to change ...                       | edit ...                                                                 | then run ...                                                                       |
|--------------------------------------------|--------------------------------------------------------------------------|------------------------------------------------------------------------------------|
| a VM's IP / MAC / port allocation          | `ops/ipam/default.nix`                                                   | `nix-build -A ops.nixos.<vm>.deploy && ./result/bin/deploy` (and medano)           |
| who can log into a VM                      | `ops/acl/default.nix` (and `users/<name>/keys/`)                         | redeploy the affected VM                                                           |
| a public-facing port forward / DNAT        | `ops/machines/x86/medano/networking.nix` (`natFlows`)                    | `nix-build -A ops.nixos.medano.deploy && ./result/bin/deploy`                      |
| reverse-proxy / TLS for a public hostname  | `ops/vms/x86/naraj/default.nix`                                          | `nix-build -A ops.nixos.naraj.deploy && ./result/bin/deploy`                       |
| an SSO policy / add an OIDC client         | `ops/vms/x86/auth/{authelia.nix,oidc_clients/}`                          | redeploy `auth` (and the client VM if it needs a new secret)                       |
| Authelia issuer / signing keys             | `ops/secrets/authelia_*.age` (`ragenix --edit`), then rekey              | `cd ops/secrets && ragenix -r`, then redeploy `auth` + every OIDC-client VM        |
| a VM's backup paths                        | `ops/vms/x86/<vm>/default.nix` (`vmBackups.paths = ...`)                 | redeploy that VM                                                                   |
| a VM's libvirt domain (RAM, vCPUs, bridge) | `ops/vms/x86/<vm>/libvirt.nix`                                           | redeploy medano                                                                    |
| add a brand-new VM                         | see "Bootstrapping a new VM" above                                       | - |
| caladan (this Mac)                         | `ops/machines/aarch64/caladan/`, `ops/darwin.nix`                        | `nix-build -A ops.nixos.caladan.deploy && ./result/bin/deploy` (or darwin-rebuild) |
| status-board scraping / inventory          | `tools/status-board/*.go` + medano module wiring                         | rebuild status-board, redeploy medano                                              |
| pinned upstream (nixpkgs, nixvirt, agenix) | `npins/sources.json` via `npins update <name>`                           | fleet rebuild as needed                                                            |
| a user-side static site / personal project | `users/hanemile/...`                                                     | per-project; e.g. `nix-build -A users.hanemile.emile_space.deploy`                 |
| an agenix secret                           | `ops/secrets/<file>.age` via `ragenix --edit`                            | redeploy the consumer VM                                                           |
| add a recipient for secrets                | `ops/secrets/secrets.nix`                                                | `cd ops/secrets && ragenix -r`, then redeploy affected VMs                         |
