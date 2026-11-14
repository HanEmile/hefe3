// Package gw wires the HAL together with the packet forwarder and consumers:
// it owns all access to the serial bridge (the MCU protocol is strictly
// request/response and not goroutine-safe), runs a single RX polling loop and
// fans received packets out to subscribers.
package gw

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/HanEmile/lorahal/hal"
)

// LogEntry is one frame in the in-memory log (RX or TX).
type LogEntry struct {
	Seq  uint64
	Time time.Time
	Dir  string // "rx" | "tx"

	Rx    *hal.RxPacket
	Tx    *hal.TxPacket
	TxErr string // set if the TX (after retry) failed
	TxToa uint32 // ms
}

// Config is the full concentrator configuration.
type Config struct {
	Board hal.BoardConf
	RxRF  [2]hal.RxRFConf
	RxIF  [10]hal.RxIFConf
	Gain  hal.TxGainLUT
}

// DefaultEU868 returns the standard EU868 configuration (8 multi-SF 125kHz
// channels on 867.1-868.7, both SX1257 radios, TX on radio 0).
func DefaultEU868() Config {
	c := Config{
		Board: hal.BoardConf{LorawanPublic: true, ClkSrc: 1},
		Gain:  hal.DefaultTxGainLUT(),
	}
	c.RxRF[0] = hal.RxRFConf{Enable: true, FreqHz: 867500000, RssiOffset: -215.4, Type: hal.RadioTypeSX1257, TxEnable: true}
	c.RxRF[1] = hal.RxRFConf{Enable: true, FreqHz: 868500000, RssiOffset: -215.4, Type: hal.RadioTypeSX1257, TxEnable: false}
	ifs := []struct {
		rf   uint8
		freq int32
	}{
		{0, -400000}, {0, -200000}, {0, 0}, {0, 200000},
		{1, -400000}, {1, -200000}, {1, 0}, {1, 200000},
	}
	for i, s := range ifs {
		c.RxIF[i] = hal.RxIFConf{Enable: true, RFChain: s.rf, FreqHz: s.freq, Bandwidth: hal.BW125kHz, Datarate: hal.DRLoraMulti}
	}
	c.RxIF[8] = hal.RxIFConf{Enable: false, RFChain: 1, FreqHz: -100000, Bandwidth: hal.BW250kHz, Datarate: hal.DRLoraSF7}
	c.RxIF[9] = hal.RxIFConf{Enable: false, RFChain: 1, FreqHz: 300000, Bandwidth: hal.BW250kHz, Datarate: 64000}
	return c
}

// MeshWatch returns a configuration tuned to listen for Meshtastic's EU868
// "Long Fast" default channel: SF11 / BW 250 kHz / CR 4/5, sync word 0x2B,
// centered on 869.525 MHz
// (meshtastic/firmware: src/mesh/MeshRadio.h modemPresetToParams() default arm
// LONG_FAST → sf=11, bw=250kHz, cr=5; src/mesh/RadioLibInterface.h syncWord
// 0x2B; src/mesh/RadioInterface.cpp regions[] EU_868 {869.4..869.65 MHz} →
// freq = 869.4 + BW/2 = 869.525 MHz).
//
// Differences from DefaultEU868:
//   - lorawan_public=false. The SX1301 only knows two preamble-sync modes
//     (LoRaWAN public ≈ 0x34 / private ≈ 0x12 FRAME_SYNCH peak pairs); 0x2B
//     matches neither. Private mode is the best-effort fallback — decode
//     degrades to "heuristic preamble lock" and weak packets may be missed.
//   - Radio 1 moves 868.5 → 869.2 MHz so the 869.525 MHz mesh channel lands
//     at IF +325 kHz on the std (IF8) chain: |IF| + BW/2 = 450 kHz fits the
//     500 kHz BW250 RF window (rf1=868.5 would need IF +1025 kHz — out of
//     band). The multi-SF channels on rf1 are therefore no longer TTN-valid;
//     mesh mode is opt-in, radio 0 keeps the TTN 867.x plan intact.
//   - IF8 (std LoRa modem) is locked to SF11/BW250 at that IF. The 8 multi-SF
//     chains stay enabled (harmless); IF9 (FSK) stays disabled.
func MeshWatch() Config {
	c := DefaultEU868()
	c.Board.LorawanPublic = false
	c.RxRF[1].FreqHz = 869200000
	c.RxIF[8] = hal.RxIFConf{Enable: true, RFChain: 1, FreqHz: 325000, Bandwidth: hal.BW250kHz, Datarate: hal.DRLoraSF11}
	return c
}

// Gateway owns the HAL and fan-out of RX packets.
type Gateway struct {
	mu        sync.Mutex // guards everything below + all HAL calls
	path      string
	H         *hal.Hal
	connected bool
	err       error

	rxCancel chan struct{}
	lastLog  []LogEntry
	seq      uint64
	maxLog   int
	subs     []func([]hal.RxPacket)

	lastCfg     Config
	hasCfg      bool
	rxErrStreak int
	recoveries  uint64

	rxTotal   uint64
	rxOK      uint64
	rxBad     uint64
	txRetries uint64
	txFail    uint64

	history *HistoryWriter
}

// New creates a Gateway (not yet connected). maxLog is the RX log capacity.
// If logDir is non-empty, frames are appended to daily JSONL files there and
// the last maxLog entries from today/yesterday are preloaded.
func New(maxLog int, logDir string) *Gateway {
	g := &Gateway{maxLog: maxLog}
	if logDir != "" {
		w, err := NewHistoryWriter(logDir)
		if err != nil {
			log.Printf("gw: history disabled: %v", err)
		} else {
			g.history = w
			old, lastSeq, err := LoadRecent(logDir, maxLog)
			if err == nil && len(old) > 0 {
				g.lastLog = old
				g.seq = lastSeq
				log.Printf("gw: preloaded %d history frames (up to seq %d)", len(old), lastSeq)
			}
		}
	}
	return g
}

// appendEntry registers a frame in the ring and the persistent history.
// Called with the mutex already held except from Connect paths - this helper
// takes the lock itself.

func (g *Gateway) appendEntry(e LogEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.appendEntryLocked(e)
}

func (g *Gateway) appendEntryLocked(e LogEntry) {
	e.Seq = g.seq
	g.seq++
	g.lastLog = append(g.lastLog, e)
	if len(g.lastLog) > g.maxLog {
		g.lastLog = g.lastLog[len(g.lastLog)-g.maxLog:]
	}
	if g.history != nil {
		if err := g.history.Append(recordFromEntry(e)); err != nil {
			log.Printf("gw: history append failed: %v", err)
		}
	}
}

// Subscribe registers a fan-out callback for RX batches. Called with at most
// one concurrent invocation; callbacks should be fast.
func (g *Gateway) Subscribe(fn func([]hal.RxPacket)) {
	g.mu.Lock()
	g.subs = append(g.subs, fn)
	g.mu.Unlock()
}

// Connect opens the device and validates MCU/chip versions.
func (g *Gateway) Connect(path string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.connected {
		return fmt.Errorf("already connected")
	}
	h, err := hal.Connect(path)
	if err != nil {
		g.err = err
		return err
	}
	g.H = h
	g.path = path
	g.connected = true
	g.err = nil
	return nil
}

// Disconnect stops RX and closes the device (optionally resetting the MCU).
func (g *Gateway) Disconnect(resetMCU bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopRxLocked()
	if g.H != nil {
		g.H.Close(resetMCU)
	}
	g.H = nil
	g.connected = false
	g.path = ""
}

// Connected reports connection state.
func (g *Gateway) Connected() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.connected
}

// Started reports whether the concentrator is started.
func (g *Gateway) Started() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.connected && g.H.Started()
}

// Path returns the currently connected device path.
func (g *Gateway) Path() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.path
}

// Info returns static device identification (and latches an error if any).
func (g *Gateway) Info() (uid [8]byte, mcuFW uint32, chipVer int, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.connected {
		return uid, 0, 0, false
	}
	return g.H.MCUUid, g.H.MCUFWUsed, g.H.SX1301Ver, true
}

// ApplyConfigAndStart applies cfg and starts the concentrator, then the RX loop.
func (g *Gateway) ApplyConfigAndStart(cfg Config) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.connected {
		return fmt.Errorf("not connected")
	}
	g.stopRxLocked()
	if g.H.Started() {
		if err := g.H.Stop(); err != nil {
			return err
		}
	}
	if err := g.H.BoardSetconf(cfg.Board); err != nil {
		return err
	}
	for ch := 0; ch < 2; ch++ {
		if err := g.H.RxrfSetconf(uint8(ch), cfg.RxRF[ch]); err != nil {
			return fmt.Errorf("rxrf %d: %w", ch, err)
		}
	}
	for ch := 0; ch < 10; ch++ {
		if err := g.H.RxifSetconf(uint8(ch), cfg.RxIF[ch]); err != nil {
			return fmt.Errorf("rxif %d: %w", ch, err)
		}
	}
	if err := g.H.TxgainSetconf(cfg.Gain); err != nil {
		return err
	}
	if err := g.H.Start(); err != nil {
		return err
	}
	g.lastCfg = cfg
	g.hasCfg = true
	g.rxErrStreak = 0
	g.startRxLocked()
	return nil
}

// Stop stops the concentrator.
func (g *Gateway) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.connected {
		return fmt.Errorf("not connected")
	}
	g.stopRxLocked()
	return g.H.Stop()
}

func (g *Gateway) stopRxLocked() {
	if g.rxCancel != nil {
		close(g.rxCancel)
		g.rxCancel = nil
	}
}

func (g *Gateway) startRxLocked() {
	g.rxCancel = make(chan struct{})
	go g.rxLoop(g.rxCancel)
}

func (g *Gateway) rxLoop(stop chan struct{}) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		g.mu.Lock()
		if g.H == nil || !g.H.Started() {
			g.mu.Unlock()
			return
		}
		pkts, err := g.H.Receive(16)
		if err != nil {
			// Serial bridge hiccup or USB bounce: escalate gradually —
			// a few retries, then a soft reset, then a full reconnect.
			g.rxErrStreak++
			streak := g.rxErrStreak
			if streak == 25 {
				g.mu.Unlock()
				g.recover()
				continue
			}
			g.mu.Unlock()
			if streak%5 == 0 {
				log.Printf("gw: receive error (streak %d): %v", streak, err)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		g.rxErrStreak = 0
		g.mu.Unlock()
		if len(pkts) == 0 {
			continue
		}
		g.mu.Lock()
		g.rxTotal += uint64(len(pkts))
		for _, p := range pkts {
			if p.Status == hal.StatCRCOK || p.Status == hal.StatNoCRC {
				g.rxOK++
			} else {
				g.rxBad++
			}
			ep := p
			g.appendEntryLocked(LogEntry{Time: time.Now(), Dir: "rx", Rx: &ep})
		}
		subs := append([]func([]hal.RxPacket){}, g.subs...)
		g.mu.Unlock()
		for _, fn := range subs {
			fn(pkts)
		}
	}
}

// recover performs a full reconnect + reconfigure + start after persistent
// bridge errors. Serialized through the mutex; called at most once per streak
// window from the RX loop.
func (g *Gateway) recover() {
	log.Printf("gw: persistent bridge errors, recovering concentrator")
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recoveries++
	g.stopRxLocked()
	path := g.path
	if g.H != nil {
		g.H.Close(false)
		g.H = nil
		g.connected = false
	}
	time.Sleep(2 * time.Second)
	if !g.hasCfg {
		log.Printf("gw: recovery aborted: no last configuration")
		return
	}
	h, err := hal.Connect(path)
	if err != nil {
		log.Printf("gw: recovery reconnect failed: %v", err)
		return
	}
	g.H = h
	g.connected = true
	if err := g.H.BoardSetconf(g.lastCfg.Board); err != nil {
		log.Printf("gw: recovery boardsetconf failed: %v", err)
		return
	}
	for ch := 0; ch < 2; ch++ {
		if err := g.H.RxrfSetconf(uint8(ch), g.lastCfg.RxRF[ch]); err != nil {
			log.Printf("gw: recovery rxrf failed: %v", err)
			return
		}
	}
	for ch := 0; ch < 10; ch++ {
		if err := g.H.RxifSetconf(uint8(ch), g.lastCfg.RxIF[ch]); err != nil {
			log.Printf("gw: recovery rxif failed: %v", err)
			return
		}
	}
	if err := g.H.TxgainSetconf(g.lastCfg.Gain); err != nil {
		log.Printf("gw: recovery txgain failed: %v", err)
		return
	}
	if err := g.H.Start(); err != nil {
		log.Printf("gw: recovery start failed: %v", err)
		return
	}
	g.rxErrStreak = 0
	g.startRxLocked()
	log.Printf("gw: recovery successful, concentrator restarted")
}

// Counters returns packet/error/self-heal counters.
func (g *Gateway) Counters() (rxTotal, rxOK, rxBad, recoveries uint64, errStreak int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rxTotal, g.rxOK, g.rxBad, g.recoveries, g.rxErrStreak
}

// TxCounters returns TX-related counters.
func (g *Gateway) TxCounters() (retries, failures uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.txRetries, g.txFail
}

// PacketsSince returns RX log entries with Seq > since.
func (g *Gateway) PacketsSince(since uint64) []LogEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]LogEntry, 0)
	for _, e := range g.lastLog {
		if e.Seq > since {
			out = append(out, e)
		}
	}
	return out
}

// Send transmits a packet. A failing MCU ACK after the concentrator has been
// up for a while is handled by one full self-heal cycle + single retry.
// Every attempt (success or failure) is recorded in the frame log.
func (g *Gateway) Send(pkt hal.TxPacket) error {
	g.mu.Lock()
	if !g.connected {
		g.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	err := g.H.Send(pkt)
	g.mu.Unlock()
	if err != nil {
		log.Printf("gw: tx failed (%v), running self-heal and retrying once", err)
		g.txRetries++
		g.recover()
		g.mu.Lock()
		if !g.connected {
			g.txFail++
			err = fmt.Errorf("tx retry after self-heal: not connected")
		} else {
			err = g.H.Send(pkt)
			if err != nil {
				g.txFail++
			}
		}
		g.mu.Unlock()
	}
	// log the TX frame (fskSyncWordSize matters only for FSK; LoRa default)
	pkt.Size = uint16(len(pkt.Payload))
	toa := hal.TimeOnAir(pkt, 3)
	ep := pkt
	e := LogEntry{Time: time.Now(), Dir: "tx", TxToa: toa, Tx: &ep}
	if err != nil {
		e.TxErr = err.Error()
	}
	g.appendEntry(e)
	return err
}

// AbortTx aborts TX.
func (g *Gateway) AbortTx() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.connected {
		return fmt.Errorf("not connected")
	}
	return g.H.AbortTx()
}

// TxStatus returns TX modem status.
func (g *Gateway) TxStatus() (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.connected {
		return hal.TxOff, nil
	}
	return g.H.TxStatus()
}

// DiagStats is a snapshot of raw hardware counters.
type DiagStats struct {
	Timestamp int32 `json:"timestamp"`
	FIFO      int32 `json:"fifo"`
	Hdr0      int32 `json:"hdr0"`
	Pkt0      int32 `json:"pkt0"`
	HdrMB     int32 `json:"hdrMB"`
	PktMB     int32 `json:"pktMB"`
	HdrFSK    int32 `json:"hdrFSK"`
	PktFSK    int32 `json:"pktFSK"`
	ChannRSSI int32 `json:"channRSSI"`
	BBRSSI    int32 `json:"bbRSSI"`
	DecRSSI   int32 `json:"decRSSI"`
}

// Diag returns a snapshot of raw hardware counters.
func (g *Gateway) Diag() (*DiagStats, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.H == nil || !g.H.Started() {
		return nil, fmt.Errorf("not started")
	}
	r := func(id int) int32 {
		v, err := g.H.RegR(id)
		if err != nil {
			return -1
		}
		return v
	}
	return &DiagStats{
		Timestamp: r(hal.LGW_TIMESTAMP),
		FIFO:      r(hal.LGW_RX_PACKET_DATA_FIFO_NUM_STORED),
		Hdr0:      r(hal.LGW_VALID_HEADER_COUNTER_0),
		Pkt0:      r(hal.LGW_VALID_PACKET_COUNTER_0),
		HdrMB:     r(hal.LGW_VALID_HEADER_COUNTER_MBWSSF),
		PktMB:     r(hal.LGW_VALID_PACKET_COUNTER_MBWSSF),
		HdrFSK:    r(hal.LGW_VALID_HEADER_COUNTER_FSK),
		PktFSK:    r(hal.LGW_VALID_PACKET_COUNTER_FSK),
		ChannRSSI: r(hal.LGW_CHANN_RSSI),
		BBRSSI:    r(hal.LGW_BB_RSSI),
		DecRSSI:   r(hal.LGW_DEC_RSSI),
	}, nil
}

// TrigCount returns the internal µs counter.
func (g *Gateway) TrigCount() (uint32, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.connected {
		return 0, fmt.Errorf("not connected")
	}
	return g.H.TrigCount()
}
