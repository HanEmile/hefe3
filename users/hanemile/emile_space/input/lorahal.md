(meta
  (tag "project")
  (published "2026-11-14")
  (modified "2026-11-15")
  (author "Emile")
  (info "tag")
)

# lorahal: my LoRa gateway work, but as a library

| The R11e-LR8 is a miniPCIe LoRaWAN concentrator card by MikroTik, built around the Semtech SX1301 baseband chip and two SX1257 radio frontends. The marketing says: put it in a RouterBoard, run RouterOS. The forums say: desolder, wire the SPI pins to your Raspberry Pi, hope. Both of those turn out to be outdated. The card I have on my desk has an STM32 microcontroller on board, and that STM32 does USB. On a two euro mPCIe-to-USB adapter, the card shows up as a plain serial device. It works in any machine with a USB port. This post is about what it took to drive it properly, and about the Go library that came out of it: github.com/HanEmile/lorahal.

(toc)

## The card

So, some hardware facts first. The SX1301 is the classic LoRa gateway chip: eight parallel demodulators (spreading factors 7 to 12, simultaneously, across eight channels), one extra "standard" channel modem, an FSK modem, and a 32 MHz fast-sample path from two external SX125x radios. It needs firmware blobs loaded at boot (a calibration firmware, an AGC loop firmware, and a packet arber firmware for its two internal 8051-style microcontrollers, funny little things).

Every document about the R11e-LR8 that I could find (the MikroTik help pages, the OpenWrt forum, a couple of order docs from people trying this in routers) says the same thing: miniPCIe connector, but the data goes over SPI on the usually-unused pins, so it works in RouterBoards and everywhere else you solder. MikroTik's own marketing also says "works in any of our devices with a USB slot" which seems weird until you realize: if there is an STM32 behind the chip, USB->SPI boards on their side can route USB. So they meant their own adapters.

## Plugging it into a random PC

So what do we get when we plug it into a normal computer? Nothing, at first. No PCIe device (correct, no such thing on this card). No USB device (also correct until someone routes it). But the STM32 on my card enumerates as `2cd2:020a MikroTik R11e-LR8` after a short warmup (it first presents as "LoRa GW Bootloader" and then reboots into the actual application, taylored for firmware updates without plugging anything):

```
$ lsusb
... cdc_acm 3-5.3:1.0: ttyACM0: USB ACM device
... Product: R11e-LR8
```

Now there is a microcontroller reachable over CDC-ACM, 115200 baud, and it speaks a protocol that Semtech invented for an old dev kit called the PicoCell GW. This is the luckiest accident in this whole project: the only publicly documented protocol around this bridge family is Semtech's own half-open C library "picoGW_hal", and its STM32 firmware command set is very close to what the MikroTik card implements.

## The handshake

The framing is simple. Host writes `[cmd][len_hi][len_lo][addr][data...]`, the MCU answers with `[cmd][len_hi][len_lo][status][data...]`. Register ops are 'r'/'w' single byte, bursts are 'a' atomic write and 'x'/'y'/'z' or 's'/'t'/'u'/'p' chunks of 600 (TX) or 900 (RX) bytes, because that is what fits the MCU's internal buffers. On top of register IO there are MCU commands: 'i' board config, 'c' radio config, 'd' intermediate-frequency chain config, 'h' TX gain table, 'b' fetch received packets, 'f' transmit a packet, 'q' read the internal microsecond counter, 'j' store radio calibration, 'l' bridge firmware negotiation, 'm' reset the bridge, 'n' jump to bootloader.

The 'l' command deserves its own paragraph. The host sends its *expectation* of the MCU firmware version as a 32 bit word. The MCU ACKs if it accepts that expectation, and answers with an 8 byte unique ID. Semtech's reference bumps this expectation every release (they named it STM32FWVERSION, cute). The MikroTik-flavored card I have expects 0x010a0006; people on the MikroTik forums had older units wanting 0x010a0004. So the Go driver probes a short list of known versions and records which one worked. Until you guess right, the MCU happily refuses every further conversation with a clean `ACK_KO` and no other diagnostics.

Once past the handshake, reading the chip version register (address 1) tells you `103`, which is SX1301 silicon revision you're probably going to see on every iC880a-style board ever.

## A look around the chip

The starting sequence is the part of the original C code that takes the most care to port, and it is also the most interesting to watch. It goes roughly:

1. soft-reset the SX1301, gate all clocks off
2. power up the two radios, program their PLLs to the RX frequencies, wait for lock
3. upload the 8 KB calibration firmware to the SX1301's AGC microcontroller, and let it do its magic for about 2.3 seconds: it wiggles radio registers and its own RAM to figure out IQ imbalance and DC offsets
4. read back 32 weird offset values from the MCU's data memory (this becomes important later if you want good transmit power accuracy, which I did not touch)
5. dump the same dance again with a second calibration firmware variant for another mixer range (they are apparently different enough to deserve two builds)
6. upload the AGC firmware and the arbiter firmware (8 KB each, read-back verified over the same bridge)
7. perform a small state-machine handshake over one register called RADIO_SELECT where the AGC expects you to push your TX gain lookup table, entry by entry, with `0x10`-flavored commands (`AGC_CMD_WAIT` / `AGC_CMD_ABORT`) until it reports status `0x40` (ready)

At this point the concentrator demodulates, the FIFO starts filling, and the rest is polling 'b' for packets and 'f' for transmitting. The packets arrive in a nice little struct (44 byte metadata + payload): frequency, direction, which IF chain, which radio, SNR minimum/mean/max, RSSI, internal timestamp, CRC state, raw bytes. Exactly what you'd want for a Wireshark-like view.

## Quirks learned the hard way

A couple of things I did not find documented anywhere, in case someone else ends up on this path:

- **The C reference has a packet-size bug that happens to never trigger.** Parsing `nb_packet`-multiplexed answers computes packet size as `(uint8_t)(ans[42] << 8) | ans[43]`. The shifted byte is silently zero. So every packet is at most 255 bytes long, forever, which in LoRaWAN is also correct in practice. Fun to stare at in a debugger for ten minutes.
- **The MCU answers desynchronize if you open the port twice.** Two readers split the byte stream, both see garbage, and neither reports the problem. My driver now holds a lock around every command/answer pair.
- **Resetting the MCU bounces USB for about twenty seconds.** It comes back eventually, but a "clean close" is not "reset the bridge and quit", it is "just close the fd".
- **After some hours of uptime a TX command occasionally gets NAKed.** No error detail, just `ACK_KO`. My gateway self-heal (reconnect, reconfigure, restart, retry) hides it; I instrumented it and am watching the counters. If you know why, email me.
- **The band-edge PLL lock is slower than setup-time PLL lock.** Also odd: at 862 MHz (where EU868 technically starts) the SX1257 takes far longer to lock than the five attempts the reference allows for. Fifteen is fine. Also: while the concentrator runs, the AGC MCU owns the radio SPI bus, and any host-side radio programming races it silently. Claim the bus (`FORCE_HOST_RADIO_CTRL`) before touching radios at runtime.

## The library

So the port landed as github.com/HanEmile/lorahal, two packages:

- `bridge`: the serial wire protocol (open, lock, framed commands, burst chunking, resync). Around 200 lines plus tests against a fake serial port.
- `hal`: everything else. Connect (handshake + version check), configuration (board, both radios, ten IF chains, TX gain LUT), Start (calibration x2 + firmware x2 + AGC handshake), Receive, Send, TimeOnAir, and a spectral scan I added because RSSI per frequency bin over the whole band is fun.

BSD-3 like Semtech's originals. The firmware blobs are embedded byte-for-byte from picoGW_hal (they are loose .bin files in the repo, 4 x 8 KB), plus the register map which I generated once from the C file by script.

A minimal hello-world-ish program is thirty lines: open, configure both radios, drop in an EU868 channel plan, Start, then poll for frames. The example in the repo is exactly that.

## What runs on it

The actual user of the library is a small gateway daemon (lora8) that currently does the following on an N100 NixOS box at my window sill: EU868 concentrator, Semtech UDP packet forwarding to TTN, a Semtech-UDP multiplexer feeding TTN and a self-hosted ChirpStack at the same time, prometheus metrics, JSONL history of every frame, spectral scans every 30 seconds, and a web UI that looks and acts suspiciously like Wireshark (filter expressions, click-a-field-to-filter, LoRaWAN header decoding, per-node AES decrypt when the AppSKey is mine).

The day the packet from a random neighbor node shows up in nice green rows with real SNR, I will consider this project "#done". The library is the piece someone else can reuse without caring about any of the rest. That was the point of this post.
