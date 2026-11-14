# Heltec WiFi LoRa 32 (V3) — OTAA uplink test for the lampadas gateway

Minimal bring-up: joins your TTN application and sends a 3-byte uplink
(counter + uptime) every 5 minutes. The frames go through **your** gateway,
so you can watch them in the lora8 UI *and* in the TTN console.

## A. TTN console (2 min)

1. https://eu1.cloud.thethings.network/console → **Applications** →
   **+ Create application** (name whatever, e.g. `lora8`).
2. Application → **+ Register end device**:
   - Input method: **Enter registration data manually**
   - Frequency plan: Europe 863-870 MHz (SF9 for RX2 - recommended)
   - LoRaWAN version: **MAC v1.1** (RadioLib implements LoRaWAN 1.1; if you
     prefer v1.0.x, TTN shows only AppKey — paste it into both AppKey and
     NwkKey in `config.h`).
   - Regional Parameters: **RP001 regional parameters 1.1 revision B**
   - JoinEUI: leave all zeros
   - Press **Generate** for DevEUI and AppKey → then **Register device**.
3. Copy **DevEUI**, **AppKey** (and NwkKey = same value) into `config.h`
   as C byte arrays, e.g. `0x70, 0xB3, 0xD5, ...`.

## B. Flash (Arduino IDE 2.x)

1. Boards Manager: install **esp32** core (by Espressif, ≥ 3.x) →
   select board **"WiFi LoRa 32 (V3)"**.
2. Library Manager: install **RadioLib** (≥ 7.1).
3. Copy this folder into your sketchbook, fill `config.h`, upload, open
   serial monitor @115200.

## C. Expected output

```
Joining (this can take a few tries) ...
Joined!
Uplink, counter=0
No downlink
Next uplink in 300 s
```

- TTN console Live data: join-accept + uplink every 5 min.
- lora8 UI (http://lampadas.pinto-pike.ts.net:8080): frames with
  SNR/RSSI per packet; expect strong signal (RSSI ≈ −30..−60 dBm) in-room,
  plus gateway header counters tick up in Prometheus (`lora8_headers_total`).

## Notes

- Antenna on the V3! The little u.FL whip is mandatory, otherwise the
  uplink barely reaches a few meters.
- Downlink: the node's RX windows are 1 s/2 s after uplink; the card submits
  via the forwarder (`lora8_fwd_tx_total` increments, TX happens on RF0 @
  869.525 MHz, EU868 RX2 for now).
- Duty cycle: uplink every 5 min is comfortably within the legal 1% for a
  3-byte message at any SF.
