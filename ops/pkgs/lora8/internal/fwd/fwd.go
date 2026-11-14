// Package fwd implements a Semtech UDP packet forwarder (GWMP): PUSH_DATA /
// PUSH_ACK, PULL_DATA / PULL_ACK, PULL_RESP and TXPK_ACK against a LoRaWAN
// network server such as TTN or ChirpStack.
package fwd

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"emile.space/lora8/internal/hal"
)

const (
	pushData = 0x00
	pushACK  = 0x01
	pullData = 0x02
	pullACK  = 0x04
	pullResp = 0x03
	txpkAck  = 0x05

	pushTimeout = 100 * time.Millisecond
	pullDataInt = 5 * time.Second
	statInt     = 30 * time.Second
)

// Stats reports packet counters.
type Stats struct {
	RxNb    uint32
	RxOK    uint32
	RxFw    uint32
	DwNb    uint32
	TxNb    uint32
	lastAck atomic.Int64 // unix nanos of last PUSH_ACK/PULL_ACK; 0 = never
}

// Forwarder is a Semtech UDP packet forwarder. Send is used for downlinks
// (must serialize concentrator access); uplinks arrive via OnPackets.
type Forwarder struct {
	Send func(hal.TxPacket) error
	EUI  [8]byte

	Stats Stats

	mu     sync.Mutex
	server *net.UDPAddr
	conn   *net.UDPConn
	tokenP uint16 // push token
	tokenD uint16 // pull token

	cancel  context.CancelFunc
	running bool
}

// SetServer configures the LNS address ("host:port", e.g. "eu1.cloud.thethings.network:1700").
func (f *Forwarder) SetServer(addr string) error {
	if addr == "" {
		f.mu.Lock()
		f.server = nil
		f.mu.Unlock()
		return nil
	}
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.server = a
	f.mu.Unlock()
	return nil
}

// Server returns the currently configured LNS address ("" if none).
func (f *Forwarder) Server() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.server == nil {
		return ""
	}
	return f.server.String()
}

// Start runs the forwarder loop until Stop is called.
func (f *Forwarder) Start() error {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return errors.New("forwarder already running")
	}
	f.mu.Unlock()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return err
	}
	f.conn = conn
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.running = true
	// seed PRNG-less tokens: time-based
	f.tokenP = uint16(time.Now().UnixNano())
	f.tokenD = uint16(time.Now().UnixNano() >> 8)

	go f.udpLoop(ctx)
	go f.tickLoop(ctx)
	log.Printf("fwd: started")
	return nil
}

// Stop halts the forwarder.
func (f *Forwarder) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.running {
		return
	}
	f.cancel()
	f.conn.Close()
	f.running = false
	f.Stats.lastAck.Store(0)
	log.Printf("fwd: stopped")
}

// Running reports whether the forwarder is active.
func (f *Forwarder) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *Forwarder) send(pkt []byte) {
	f.mu.Lock()
	server := f.server
	f.mu.Unlock()
	if server == nil {
		return
	}
	if _, err := f.conn.WriteToUDP(pkt, server); err != nil {
		log.Printf("fwd: udp send error: %v", err)
	}
}

// Connected reports whether an LNS ACK was seen within the last 30 s.
func (f *Forwarder) Connected() bool {
	t := f.Stats.lastAck.Load()
	return t != 0 && time.Since(time.Unix(0, t)) < 30*time.Second
}

// LastAck returns the time of the last LNS ACK (zero value if never).
func (f *Forwarder) LastAck() time.Time {
	if t := f.Stats.lastAck.Load(); t != 0 {
		return time.Unix(0, t)
	}
	return time.Time{}
}

// OnPackets forwards a batch of received packets to the LNS as rxpk JSON.
func (f *Forwarder) OnPackets(pkts []hal.RxPacket) {
	if !f.Running() {
		return
	}
	pkt := map[string]any{"rxpk": toRxpk(pkts, &f.Stats)}
	if pkt["rxpk"] == nil {
		return
	}
	f.push(pkt)
}

func toRxpk(pkts []hal.RxPacket, st *Stats) []map[string]any {
	out := make([]map[string]any, 0, len(pkts))
	for _, p := range pkts {
		atomic.AddUint32(&st.RxNb, 1)
		if p.Status != hal.StatCRCOK && p.Status != hal.StatNoCRC {
			continue
		}
		atomic.AddUint32(&st.RxOK, 1)
		o := map[string]any{
			"tmst": p.CountUs,
			"chan": p.IFChain,
			"rfch": p.RFChain,
			"freq": float64(p.FreqHz) / 1e6,
			"stat": 1,
			"rssi": round1(p.Rssi),
			"size": p.Size,
			"data": base64.StdEncoding.EncodeToString(p.Payload),
		}
		if p.Modulation == hal.ModLora {
			o["modu"] = "LORA"
			o["datr"] = fmt.Sprintf("SF%dBW%d", sfNum(p.Datarate), bwHz(p.Bandwidth)/1000)
			o["codr"] = fmt.Sprintf("4/%d", 4+int(p.Coderate))
			o["lsnr"] = round1(p.Snr)
		} else {
			o["modu"] = "FSK"
			o["datr"] = p.Datarate
		}
		atomic.AddUint32(&st.RxFw, 1)
		out = append(out, o)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func round1(v float32) float32 { return float32(math.Round(float64(v)*10)) / 10 }

func sfNum(dr uint32) int {
	switch dr {
	case hal.DRLoraSF7:
		return 7
	case hal.DRLoraSF8:
		return 8
	case hal.DRLoraSF9:
		return 9
	case hal.DRLoraSF10:
		return 10
	case hal.DRLoraSF11:
		return 11
	case hal.DRLoraSF12:
		return 12
	}
	return 0
}

func bwHz(bw uint8) int {
	switch bw {
	case hal.BW500kHz:
		return 500000
	case hal.BW250kHz:
		return 250000
	case hal.BW125kHz:
		return 125000
	case hal.BW62k5Hz:
		return 62500
	case hal.BW31k2Hz:
		return 31200
	case hal.BW15k6Hz:
		return 15600
	case hal.BW7k8Hz:
		return 7800
	}
	return 0
}

func (f *Forwarder) push(packet map[string]any) {
	f.tokenP++
	hdr := make([]byte, 12)
	hdr[0] = 2
	binary.LittleEndian.PutUint16(hdr[1:], f.tokenP)
	hdr[3] = pushData
	copy(hdr[4:], f.EUI[:])
	if packet != nil {
		payload, err := json.Marshal(packet)
		if err != nil {
			return
		}
		f.send(append(hdr, payload...))
		return
	}
	f.send(hdr)
}

func (f *Forwarder) udpLoop(ctx context.Context) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = f.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			log.Printf("fwd: udp read error: %v", err)
			continue
		}
		if n < 4 || buf[0] != 2 {
			continue
		}
		token := binary.LittleEndian.Uint16(buf[1:])
		switch buf[3] {
		case pushACK:
			if token == f.tokenP {
				f.Stats.lastAck.Store(time.Now().UnixNano())
			}
		case pullACK:
			if token == f.tokenD {
				f.Stats.lastAck.Store(time.Now().UnixNano())
			}
		case pullResp:
			f.handlePullResp(token, buf[4:n])
		}
	}
}

type txPkt struct {
	Imme *bool           `json:"imme,omitempty"`
	Tmst *uint32         `json:"tmst,omitempty"`
	Freq float64         `json:"freq"`
	RFch uint8           `json:"rfch"`
	Powe int8            `json:"powe"`
	Modu string          `json:"modu"`
	Datr json.RawMessage `json:"datr"`
	Codr string          `json:"codr,omitempty"`
	FDev *uint8          `json:"fdev,omitempty"`
	IPol *bool           `json:"ipol,omitempty"`
	Prea *uint16         `json:"prea,omitempty"`
	Size uint16          `json:"size"`
	Data string          `json:"data"`
	NCrc *bool           `json:"ncrc,omitempty"`
}

func (f *Forwarder) handlePullResp(token uint16, payload []byte) {
	var wrap struct {
		TxPkt txPkt `json:"txpk"`
	}
	if err := json.Unmarshal(payload, &wrap); err != nil {
		log.Printf("fwd: bad PULL_RESP json: %v", err)
		return
	}
	tx := wrap.TxPkt
	pkt, err := txPktFromJSON(tx)
	if err != nil {
		log.Printf("fwd: bad txpk: %v", err)
	} else {
		if err := f.Send(pkt); err != nil {
			log.Printf("fwd: lgw_send failed: %v", err)
		} else {
			atomic.AddUint32(&f.Stats.DwNb, 1)
			atomic.AddUint32(&f.Stats.TxNb, 1)
			log.Printf("fwd: txpk sent: %.3f MHz %s %d B", tx.Freq, tx.Modu, tx.Size)
		}
	}
	ack := []byte{2, byte(token), byte(token >> 8), txpkAck}
	f.send(ack)
}

// txPktFromJSON converts a GWMP txpk into a HAL packet. Exported for reuse by
// the web UI when scheduling downlinks manually.
func txPktFromJSON(tx txPkt) (hal.TxPacket, error) {
	payload, err := base64.StdEncoding.DecodeString(tx.Data)
	if err != nil {
		payload, err = hex.DecodeString(tx.Data)
		if err != nil {
			return hal.TxPacket{}, fmt.Errorf("bad payload encoding: %w", err)
		}
	}
	p := hal.TxPacket{
		FreqHz:  uint32(tx.Freq * 1e6),
		RFChain: tx.RFch,
		RFPower: tx.Powe,
		Payload: payload,
	}
	if tx.Imme != nil && *tx.Imme {
		p.TxMode = hal.TxImmediate
	} else if tx.Tmst != nil {
		p.TxMode = hal.TxTimestamped
		p.CountUs = *tx.Tmst
	} else {
		return hal.TxPacket{}, fmt.Errorf("txpk needs imme=true or tmst")
	}
	if tx.IPol != nil {
		p.InvertPol = *tx.IPol
	}
	if tx.Prea != nil {
		p.Preamble = *tx.Prea
	}
	if tx.NCrc != nil {
		p.NoCRC = *tx.NCrc
	}
	switch tx.Modu {
	case "LORA":
		p.Modulation = hal.ModLora
		var datr string
		if err := json.Unmarshal(tx.Datr, &datr); err != nil {
			return hal.TxPacket{}, fmt.Errorf("bad LORA datr: %w", err)
		}
		sf, bwk, err := parseDatr(datr)
		if err != nil {
			return hal.TxPacket{}, err
		}
		p.Datarate = sf
		p.Bandwidth = bwk
		switch tx.Codr {
		case "4/5":
			p.Coderate = hal.CRLora45
		case "4/6":
			p.Coderate = hal.CRLora46
		case "4/7":
			p.Coderate = hal.CRLora47
		case "4/8":
			p.Coderate = hal.CRLora48
		}
	case "FSK":
		p.Modulation = hal.ModFsk
		var br uint32
		if err := json.Unmarshal(tx.Datr, &br); err != nil {
			return hal.TxPacket{}, fmt.Errorf("bad FSK datr: %w", err)
		}
		p.Datarate = br
		if tx.FDev != nil {
			p.FDev = *tx.FDev
		} else {
			p.FDev = 3
		}
	default:
		return hal.TxPacket{}, fmt.Errorf("unknown modulation %q", tx.Modu)
	}
	return p, nil
}

func parseDatr(s string) (sf uint32, bw uint8, err error) {
	var sfNum, bwk int
	if _, err := fmt.Sscanf(s, "SF%dBW%d", &sfNum, &bwk); err != nil {
		return 0, 0, fmt.Errorf("bad datr %q: %w", s, err)
	}
	sf = map[uint32]uint32{7: hal.DRLoraSF7, 8: hal.DRLoraSF8, 9: hal.DRLoraSF9, 10: hal.DRLoraSF10, 11: hal.DRLoraSF11, 12: hal.DRLoraSF12}[uint32(sfNum)]
	bw = map[int]uint8{125: hal.BW125kHz, 250: hal.BW250kHz, 500: hal.BW500kHz}[bwk]
	if sf == 0 || bw == 0 {
		return 0, 0, fmt.Errorf("bad datr %q", s)
	}
	return sf, bw, nil
}

func (f *Forwarder) tickLoop(ctx context.Context) {
	statT := time.NewTicker(statInt)
	pullT := time.NewTicker(pullDataInt)
	defer statT.Stop()
	defer pullT.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-pullT.C:
			f.tokenD++
			pkt := make([]byte, 12)
			pkt[0] = 2
			binary.LittleEndian.PutUint16(pkt[1:], f.tokenD)
			pkt[3] = pullData
			copy(pkt[4:], f.EUI[:])
			f.send(pkt)
		case <-statT.C:
			stats := map[string]any{
				"stat": map[string]any{
					"time": time.Now().UTC().Format("2006-01-02 15:04:05 GMT"),
					"rxnb": atomic.LoadUint32(&f.Stats.RxNb),
					"rxok": atomic.LoadUint32(&f.Stats.RxOK),
					"rxfw": atomic.LoadUint32(&f.Stats.RxFw),
					"dwnb": atomic.LoadUint32(&f.Stats.DwNb),
					"txnb": atomic.LoadUint32(&f.Stats.TxNb),
				},
			}
			f.push(stats)
		}
	}
}
