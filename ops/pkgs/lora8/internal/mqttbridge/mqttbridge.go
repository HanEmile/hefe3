// Package mqttbridge subscribes a local MQTT broker for external radio
// traffic (Meshtastic nodes running the built-in MQTT module with the JSON
// integration) and injects those packets into the gateway's frame log,
// tagged with their own class so they stay distinguishable from card RX.
package mqttbridge

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Inject registers one frame in the in-memory log. Same shape as gw.LogEntry.
type Inject func(dir string, hexPayload string, meta map[string]any)

// Bridge is a lazy MQTT subscriber.
type Bridge struct {
	Addr   string
	Topic  string
	inject Inject
	client mqtt.Client
	linked atomic.Bool
}

// New starts a broker subscription. addr = "tcp://127.0.0.1:1883",
// topic = "msh/#" (Meshtastic default.)
func New(addr, topic string, inject Inject) (*Bridge, error) {
	b := &Bridge{Addr: addr, Topic: topic, inject: inject}
	opts := mqtt.NewClientOptions().
		AddBroker(addr).
		SetClientID(fmt.Sprintf("lora8-%d", time.Now().UnixNano()%10000)).
		SetCleanSession(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetAutoReconnect(true).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Printf("mqttbridge: disconnected: %v", err)
			b.linked.Store(false)
		}).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Printf("mqttbridge: connected, subscribing %s", topic)
			b.linked.Store(true)
			if t := c.Subscribe(topic, 0, b.onMsg); t.Wait() && t.Error() != nil {
				log.Printf("mqttbridge: subscribe %s: %v", topic, t.Error())
			}
		})
	b.client = mqtt.NewClient(opts)
	go func() {
		if t := b.client.Connect(); t.Wait() && t.Error() != nil {
			log.Printf("mqttbridge: %v (retrying in background)", t.Error())
		}
	}()
	return b, nil
}

// Linked reports broker connectivity.
func (b *Bridge) Linked() bool { return b.linked.Load() }

type jsonFrame struct {
	From     uint32 `json:"from"`
	To       uint32 `json:"to"`
	Channel  string `json:"channel"`
	HopLimit uint8  `json:"hopLimit"`
	HopStart uint8  `json:"hopStart"`
	RadioCfg struct {
		Freq float64 `json:"freq"`
		Mod  string  `json:"mod"`
	} `json:"radioCfg"`
	Portnum uint32 `json:"portnum"`
	Payload string `json:"payload"` // base64
}

// onMsg handles one incoming message; frames that look like Meshtastic
// MQTT JSON integration payloads get injected with the content payload as
// hex; everything else is injected raw for the lab view.
func (b *Bridge) onMsg(_ mqtt.Client, m mqtt.Message) {
	topic := m.Topic()

	// JSON integration path: body is {"payload": "<base64>", ...}
	var jf jsonFrame
	if err := json.Unmarshal(m.Payload(), &jf); err == nil && jf.Payload != "" {
		raw, err := base64.StdEncoding.DecodeString(jf.Payload)
		if err != nil {
			return
		}
		meta := map[string]any{"topic": topic}
		if jf.From != 0 {
			meta["mqtt_from"] = fmt.Sprintf("%x", jf.From)
			meta["mqtt_to"] = fmt.Sprintf("%x", jf.To)
			meta["mqtt_portnum"] = jf.Portnum
		}
		if jf.RadioCfg.Freq != 0 {
			meta["freq_hz"] = uint32(jf.RadioCfg.Freq * 1e6)
		}
		b.inject("rx", hex.EncodeToString(raw), meta)
		return
	}

	// Generic path: wire body as-is (meshtastic ServiceEnvelope or anything
	// else a node published); the lab view still unpacks it if structured.
	b.inject("rx", hex.EncodeToString(m.Payload()), map[string]any{"topic": topic})
}

// Close shuts the subscription down gracefully.
func (b *Bridge) Close() {
	if b.client != nil && b.client.IsConnected() {
		b.client.Disconnect(200)
	}
}

// ClassUpdate exposes a hint whether frames look like mesh mqtt traffic.
var _ = strings.Contains
