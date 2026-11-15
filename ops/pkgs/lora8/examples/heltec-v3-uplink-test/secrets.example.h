// Copy to secrets.h and fill in before compiling / flashing.
//
// WiFi OTA updates: after ONE USB flash, future firmware uploads can be done
// over WiFi: `pio run -t upload --upload-port lora8-node.local` (Arduino IDE:
// Network -> lora8-node). Password required for every upload.
#ifndef LORA8_NODE_SECRETS_H
#define LORA8_NODE_SECRETS_H

#define WIFI_SSID "your-wifi"
#define WIFI_PASS "your-password"
#define OTA_PASS  "choose-an-ota-password"

#endif
