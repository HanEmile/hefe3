# status-board

Internal dashboard for the medano fleet. One Go binary, no external deps.

- HTML page at `https://status.medano.emile.space/` (medano nginx proxies
  `127.0.0.1:8090`)
- Scrapes each VM's `:9100/metrics` (node-exporter + textfile probes) over
  tailscale.
- Calls `virsh list --all` and `virsh dominfo <name>` on medano for libvirt
  state.
- Walks `/mnt/storagebox-bx11/backup/<vm>/snapshots/` for restic snapshot
  freshness (no repo password needed - just stat the mtime of the most
  recent snapshot file).
- Renders an inline SVG showing the traffic flow:
  internet → medano nginx → bridges (default, private, rou) → VMs.

## Build

```sh
nix-build -A tools.status-board.package    # binary at result/bin/status-board
```

Run by the NixOS module at `tools.status-board.module`, imported from
`ops/machines/x86/medano/default.nix`.

## Env vars

| Var | Default | Notes |
|---|---|---|
| `STATUS_BOARD_INVENTORY` | (required) | path to a JSON file `[{name, ip, bridge}, ...]` |
| `STATUS_BOARD_LISTEN` | `127.0.0.1:8090` | bind address:port |
| `STATUS_BOARD_STORAGEBOX` | `/mnt/storagebox-bx11/backup` | base dir of restic repos |

## Output sample

The `/` endpoint returns an HTML page with:
- Header with last-scrape timestamp.
- An SVG with three rows: internet box, medano nginx box, bridge boxes,
  one row of VM boxes per bridge. Each VM box is coloured green/red/yellow
  based on probe success.
- A table: name / bridge / ip / libvirt state / max+used RAM / probe pills /
  restic snapshot count + age.

Refreshes itself every 60 seconds via meta-refresh.
