# lora8

Go driver, CLI, web UI and Semtech UDP packet forwarder for the
**MikroTik R11e-LR8** LoRaWAN concentrator card (SX1301 baseband + 2× SX1257
radios behind an on-board STM32 USB↔SPI bridge). Plugged into an
mPCIe→USB adapter it enumerates as USB CDC-ACM (`2cd2:020a`,
`/dev/ttyACM*`, bridged to `/dev/lora8` by udev on lampadas).

The card speaks the Semtech **PicoCell GW** host protocol, making it a pure
Go port of `Lora-net/picoGW_hal` (BSD):

- `internal/com` — USB CDC bridge protocol (framing, resync, burst chunking)
- `internal/hal` — SX1301 driver: connect/version check, board/radio/IF/TX-gain
  config, radio calibration, AGC/arbiter firmware upload (blobs in
  `internal/hal/fw`, 1:1 from picoGW_hal), start/stop, RX fetch, TX,
  time-on-air.
- `internal/gw` — serialized device access, single RX loop with fan-out,
  default EU868 channel plan.
- `internal/fwd` — Semtech UDP forwarder (GWMP: PUSH/PULL_DATA, PULL_RESP
  downlinks, TXPK_ACK, stat reports). Gateway EUI = MCU bridge UID.
- `internal/web` — minimal UI with live dashboard.

## CLI

```
lora8 info                          # card detection + versions
lora8 probe                         # raw hardware counters (RF health check)
lora8 listen                        # EU868 + live packet dump
lora8 send -freq 868.1 -payload CAFEBABE   # one-shot TX
lora8 fwd -server eu1.cloud.thethings.network:1700
lora8 web -addr :8080 [-auto] [-fwd host:port]
```

## Deployment

NixOS service via `ops/modules/lora8.nix` (see `services.lora8.*`), enabled on
lampadas with the TTN EU Semtech UDP endpoint. The hardware access is via a
udev rule (`ops/machines/x86/lampadas/default.nix`) symlinking the card to
`/dev/lora8` with `dialout` group access.

## Ops surface

- **Prometheus**: `GET /metrics` (bridge state, RX counters incl. CRC split,
  noise-floor registers, forwarder link health, self-heal recoveries);
  lampadas' local prometheus scrapes it as job `lora8` (retention 365 d,
  consistent with the fleet's node-exporter wiring).
- **Liveness**: `GET /healthz` (503 until connected+started).
- **Self-heal**: 25 consecutive bridge errors → full reconnect +
  reconfigure + restart, counted in `lora8_recoveries_total`. Handles USB
  bounces from MCU resets and USB-mPCIe-adapter flakiness.
- **Backups**: none — the service is stateless by design (config lives in
  nix; the RX log is an in-memory ring). If that ever changes, follow the
  `storageboxBackup` pattern in `ops/machines/x86/lampadas/default.nix`.

## Mesh mode (Meshtastic listener, best-effort)

The UI's **mesh** checkbox (concentrator card) switches the active preset to
`gw.MeshWatch()`: a Meshtastic EU868 **Long Fast** listener. Long Fast is the
Meshtastic factory default preset, so an unmodified EU868 node transmits on a
single static channel:

| parameter | value | source (meshtastic/firmware) |
|---|---|---|
| SF / BW / CR | SF11 / 250 kHz / 4/5 | `src/mesh/MeshRadio.h`, `modemPresetToParams()` (LONG_FAST fallback arm: sf=11, bw=250, cr=5) |
| sync word | `0x2B` | `src/mesh/RadioLibInterface.h` (`const uint8_t syncWord = 0x2b`) |
| center freq | **869.525 MHz** | `src/mesh/RadioInterface.cpp`: EU_868 region {869.4–869.65 MHz}, one 250 kHz slot → `freqStart + BW/2` |
| preamble | 16 syms, explicit header, CRC on | `src/mesh/RadioInterface.h` (`preambleLengthDefault = 16`) |

Radio 1 is moved to 869.2 MHz so 869.525 lands at IF +325 kHz on the std IF8
modem (|IF|+BW/2 = 450 kHz ≤ 500 kHz window). The multi-SF chains stay up;
radio 0's TTN plan is untouched (rf1's multi channels are no longer TTN-valid
while mesh is on — the toggle is opt-in).

**Caveat — sync word.** The SX1301 only knows two preamble-sync modes,
LoRaWAN *public* (≈0x34) and *private* (≈0x12), selected by
`lorawan_public` (picoGW `FRAME_SYNCH_PEAK1/2_POS` pairs 3/4 vs 1/2).
Meshtastic's 0x2B matches **neither**. Mesh mode uses private as the
least-off setting: the demodulators usually still lock on the preamble
correlator for medium/strong signals, but weak packets and some header/CRC
validations will be missed or fail. Treat mesh decode as opportunistic
sniffing, not a gateway. The payload inspector guesses the 16-byte
Meshtastic header (`src/mesh/RadioInterface.h` `PacketHeader`: to/from/id
LE32, flags, channel hash, next_hop, relay_node) and shows node IDs; the
AES-CTR(channel-PSK)-encrypted `Data` protobuf itself is displayed raw, with
plaintext extraction only for the rare unencrypted frame.

## TTN recipe

1. Register at https://eu1.cloud.thethings.network/console (free).
2. Gateways → "Register gateway": pick **"Legacy (Semtech UDP) packet
   forwarder"**, gateway EUI = the one shown in the lora8 UI header
   (this card: `50313953471A4750`).
3. Point the `lns` option at the plane's UDP endpoint
   (EU: `eu1.cloud.thethings.network:1700`) — already default on lampadas.
4. The UI's "lns link" LED goes green once PUSH_ACK/PULL_ACK arrive.
