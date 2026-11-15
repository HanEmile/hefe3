/*
  Heltec WiFi LoRa 32 (V3) — LoRaWAN OTAA uplink test for a lampadas/TTN
  gateway pair. Based on RadioLib's LoRaWAN_Starter example.

  1. Copy this whole folder into your Arduino sketchbook.
  2. Install RadioLib (>= 7.1) via Library Manager.
  3. Fill in DevEUI / AppKey / NwkKey in config.h (from the TTN console).
  4. Select board "WiFi LoRa 32 (V3)" and upload.

  The serial monitor (115200 8N1) shows join + uplink progress. The gateway
  web UI will show the frames with real SNR/RSSI in the "Received packets"
  table and the TTN application console under "Live data".
*/

#include "config.h"
#include "secrets.h"     // WIFI_SSID / WIFI_PASS / OTA_PASS (see secrets.example.h)

#include <WiFi.h>
#include <ArduinoOTA.h>

uint8_t counter = 0;

// WiFi OTA: once flashed over USB, future updates go over the air. Ping fast
// fail-safe: OTA only activates after LoRaWAN joins (or on join fail retry),
// otherwise the uplink loop is unaffected.
static void otaInit() {
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASS);
  ArduinoOTA.setHostname("lora8-node");
  ArduinoOTA.setPassword(OTA_PASS);
  ArduinoOTA
    .onStart([]() { Serial.println("OTA start"); })
    .onEnd([]() { Serial.println("OTA done"); })
    .onProgress([](unsigned int p, unsigned int t) { Serial.printf("OTA %u%%\r", p * 100 / t); })
    .onError([](ota_error_t e) { Serial.printf("OTA error %u\n", e); });
  ArduinoOTA.begin();
}

void setup() {
  Serial.begin(115200);
  while (!Serial);
  delay(2000);
  Serial.println(F("\nheltec-v3-uplink-test"));

  ConfigLoRa_t config;
  config.frequency = RADIOLIB_UNIT_MEGA(868); // region default, LoRaWAN layer overrides
  // Heltec V3: the SX1262 runs from a TCXO; 1.6 V works on the boards in the
  // fleet. If the radio init or join consistently fails, try Voltage1V8.
  radio.tcxoVoltage = RadioLibTCXOVoltage_t::Voltage1V6;
  int state = radio.begin(config);
  debug(state != RADIOLIB_ERR_NONE, F("Initialise radio failed"), state, true);

  state = node.beginOTAA(joinEUI, devEUI, nwkKey, appKey);
  debug(state != RADIOLIB_ERR_NONE, F("Initialise node failed"), state, true);

  Serial.println(F("Joining (OTAA, retries forever) ..."));
  while (true) {
    state = node.activateOTAA();
    if (state == RADIOLIB_LORAWAN_NEW_SESSION) break;
    Serial.print(F("join attempt failed: "));
    Serial.print(stateDecode(state));
    Serial.print(" (");
    Serial.print(state);
    Serial.println(") - retrying in 10 s");
    delay(10000);
  }

  Serial.println(F("Joined!\n"));
  otaInit();
  Serial.println(F("OTA over WiFi listening (ide: Network -> lora8-node, pass in secrets.h)"));
}

void loop() {
  // Counter + uptime so the payload is recognisable in the TTN console:
  // [ counter, uptime_seconds_u16le ]
  uint8_t payload[3];
  payload[0] = counter++;
  uint16_t up = (uint16_t)(millis() / 1000);
  payload[1] = highByte(up);
  payload[2] = lowByte(up);

  Serial.print(F("Uplink, counter="));
  Serial.println(payload[0]);
  int16_t state = node.sendReceive(payload, sizeof(payload));
  debug(state < RADIOLIB_ERR_NONE, F("Error in sendReceive"), state, false);
  Serial.println(state > 0 ? F("Downlink received") : F("No downlink"));

  Serial.print(F("Next uplink in "));
  Serial.print(uplinkIntervalSeconds);
  Serial.println(F(" s\n"));

  // service WiFi OTA requests between uplinks (chunk the wait to stay responsive)
  for (uint32_t i = 0; i < uplinkIntervalSeconds * 10; i++) {
    ArduinoOTA.handle();
    delay(100);
  }
}
