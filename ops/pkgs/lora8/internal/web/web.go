// Package web exposes a minimal HTTP UI on top of the gateway.
package web

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"emile.space/lora8/internal/fwd"
	"emile.space/lora8/internal/gw"
	"github.com/HanEmile/lorahal/hal"
)

//go:embed index.html
var indexHTML []byte

// Server holds the dependencies for the UI.
type Server struct {
	G   *gw.Gateway
	Fwd *fwd.Forwarder

	StartedAt time.Time

	mu  sync.Mutex
	Cfg gw.Config
}

// logRing keeps the most recent log lines for the UI's Logs card.
type logRing struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// LogWriter tees primary log output into the shared ring while still writing
// to stderr/journald.
func LogWriter(w io.Writer) io.Writer { return io.MultiWriter(w, ringWriter) }

var ringWriter = &logRing{max: 200}

func (l *logRing) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := strings.TrimRight(string(p), "\n")
	if line != "" {
		l.lines = append(l.lines, line)
		if len(l.lines) > l.max {
			l.lines = l.lines[len(l.lines)-l.max:]
		}
	}
	return len(p), nil
}

func (l *logRing) Tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n > len(l.lines) {
		n = len(l.lines)
	}
	out := make([]string, n)
	copy(out, l.lines[len(l.lines)-n:])
	return out
}

// New creates the UI server bound to g/f. cfg is the working configuration
// edited through the UI.
func New(g *gw.Gateway, f *fwd.Forwarder, cfg gw.Config) *Server {
	s := &Server{G: g, Fwd: f, Cfg: cfg}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, 500, map[string]string{"error": err.Error()})
}

// Handler returns the HTTP handler for the UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/connect", s.connect)
	mux.HandleFunc("POST /api/disconnect", s.disconnect)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("POST /api/config", s.setConfig)
	mux.HandleFunc("POST /api/start", s.start)
	mux.HandleFunc("POST /api/stop", s.stop)
	mux.HandleFunc("POST /api/tx", s.tx)
	mux.HandleFunc("POST /api/aborttx", s.abortTx)
	mux.HandleFunc("GET /api/packets", s.packets)
	mux.HandleFunc("GET /api/diag", s.diag)
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/logs", s.logs)
	mux.HandleFunc("POST /api/fwd", s.fwdCtl)
	mux.HandleFunc("GET /api/fwd", s.fwdInfo)
	return mux
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	uid, mcuFW, chip, ok := s.G.Info()
	trig, _ := s.G.TrigCount()
	txs, _ := s.G.TxStatus()
	uptime := 0.0
	if !s.StartedAt.IsZero() {
		uptime = time.Since(s.StartedAt).Seconds()
	}
	writeJSON(w, 200, map[string]any{
		"connected": ok,
		"uptime_s":  uptime,
		"path":     s.G.Path(),
		"mcu_uid":   hex.EncodeToString(uid[:]),
		"eui":       strings.ToUpper(hex.EncodeToString(uid[:])),
		"mcu_fw":    fmt.Sprintf("0x%08X", mcuFW),
		"chip":      chip,
		"started":   s.G.Started(),
		"trigcnt":   trig,
		"txstatus":  txs,
	})
}

// diag returns raw hardware counters for the live dashboard.
func (s *Server) diag(w http.ResponseWriter, r *http.Request) {
	d, err := s.G.Diag()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, d)
}

type connectReq struct {
	Path string `json:"path"`
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var req connectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.G.Connect(req.Path); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) disconnect(w http.ResponseWriter, r *http.Request) {
	s.G.Disconnect(false)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, 200, s.Cfg)
}

func (s *Server) setConfig(w http.ResponseWriter, r *http.Request) {
	var cfg gw.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, err)
		return
	}
	if cfg.Gain.Size < 1 {
		cfg.Gain = hal.DefaultTxGainLUT()
	}
	s.mu.Lock()
	s.Cfg = cfg
	s.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.Cfg
	s.mu.Unlock()
	if err := s.G.ApplyConfigAndStart(cfg); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	if err := s.G.Stop(); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type txReq struct {
	FreqMHz float64 `json:"freq_mhz"`
	RFChain int     `json:"rf_chain"`
	Power   int8    `json:"power"`
	SF      int     `json:"sf"`
	BW      int     `json:"bw_khz"`
	CR      int     `json:"cr"`
	Payload string  `json:"payload_hex"`
	CountMs uint32  `json:"count_ms"`
	Mode    string  `json:"mode"` // "immediate" | "timestamped"
}

func (s *Server) tx(w http.ResponseWriter, r *http.Request) {
	var req txReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	payload, err := hex.DecodeString(req.Payload)
	if err != nil {
		writeErr(w, fmt.Errorf("payload must be hex: %w", err))
		return
	}
	pkt := hal.TxPacket{
		FreqHz:     uint32(req.FreqMHz * 1e6),
		RFChain:    uint8(req.RFChain),
		RFPower:    req.Power,
		Modulation: hal.ModLora,
		Coderate:   uint8(req.CR),
		Payload:    payload,
	}
	pkt.Datarate = map[int]uint32{7: hal.DRLoraSF7, 8: hal.DRLoraSF8, 9: hal.DRLoraSF9, 10: hal.DRLoraSF10, 11: hal.DRLoraSF11, 12: hal.DRLoraSF12}[req.SF]
	pkt.Bandwidth = map[int]uint8{125: hal.BW125kHz, 250: hal.BW250kHz, 500: hal.BW500kHz}[req.BW]
	if pkt.Datarate == 0 || pkt.Bandwidth == 0 {
		writeErr(w, fmt.Errorf("invalid SF or BW"))
		return
	}
	if req.Mode == "timestamped" {
		pkt.TxMode = hal.TxTimestamped
		pkt.CountUs = req.CountMs * 1000
	} else {
		pkt.TxMode = hal.TxImmediate
	}
	toa := hal.TimeOnAir(pkt, 3)
	if err := s.G.Send(pkt); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "toa_ms": toa})
}

func (s *Server) abortTx(w http.ResponseWriter, r *http.Request) {
	if err := s.G.AbortTx(); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) packets(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	entries := s.G.PacketsSince(since)
	res := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		f := map[string]any{
			"seq":  e.Seq,
			"time": e.Time.Format("15:04:05.000"),
			"dir":  e.Dir,
		}
		if e.Dir == "rx" && e.Rx != nil {
			p := e.Rx
			f["freq_mhz"] = math.Round(float64(p.FreqHz)/1e4) / 1e2
			f["if"] = int(p.IFChain)
			f["rf"] = int(p.RFChain)
			f["mod"] = "LORA"
			if p.Modulation == hal.ModLora {
				f["sf"] = dr2sf(p.Datarate)
				f["bw"] = bw2khz(p.Bandwidth)
			} else {
				f["mod"] = "FSK"
				f["br"] = int(p.Datarate)
			}
			f["cr"] = int(p.Coderate)
			f["rssi"] = math.Round(float64(p.Rssi)*10) / 10
			f["snr"] = math.Round(float64(p.Snr)*10) / 10
			f["crc"] = map[uint8]string{hal.StatCRCOK: "ok", hal.StatCRCBad: "bad", hal.StatNoCRC: "nocrc"}[p.Status]
			f["size"] = int(p.Size)
			f["hex"] = hex.EncodeToString(p.Payload)
		}
		if e.Dir == "tx" && e.Tx != nil {
			p := e.Tx
			f["freq_mhz"] = math.Round(float64(p.FreqHz)/1e4) / 1e2
			f["rf"] = int(p.RFChain)
			f["mod"] = "LORA"
			if p.Modulation == hal.ModLora {
				f["sf"] = dr2sf(p.Datarate)
				f["bw"] = bw2khz(p.Bandwidth)
			} else {
				f["mod"] = "FSK"
				f["br"] = int(p.Datarate)
			}
			f["cr"] = int(p.Coderate)
			f["power"] = int(p.RFPower)
			f["toa_ms"] = e.TxToa
			f["size"] = int(p.Size)
			f["hex"] = hex.EncodeToString(p.Payload)
			if e.TxErr != "" {
				f["status"] = "failed"
				f["err"] = e.TxErr
			} else {
				f["status"] = "ok"
			}
		}
		res = append(res, f)
	}
	writeJSON(w, 200, res)
}

func bw2khz(bw uint8) int {
	switch bw {
	case hal.BW500kHz:
		return 500
	case hal.BW250kHz:
		return 250
	case hal.BW125kHz:
		return 125
	case hal.BW62k5Hz:
		return 62
	case hal.BW31k2Hz:
		return 31
	case hal.BW15k6Hz:
		return 15
	case hal.BW7k8Hz:
		return 7
	}
	return 0
}

func dr2sf(dr uint32) int {
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

func (s *Server) fwdInfo(w http.ResponseWriter, r *http.Request) {
	la := ""
	if t := s.Fwd.LastAck(); !t.IsZero() {
		la = t.Format("15:04:05")
	}
	writeJSON(w, 200, map[string]any{
		"running":   s.Fwd.Running(),
		"server":    s.Fwd.Server(),
		"connected": s.Fwd.Connected(),
		"last_ack":  la,
		"rxnb":      s.Fwd.Stats.RxNb,
		"rxok":      s.Fwd.Stats.RxOK,
		"rxfw":      s.Fwd.Stats.RxFw,
		"dwnb":      s.Fwd.Stats.DwNb,
		"txnb":      s.Fwd.Stats.TxNb,
	})
}

type fwdReq struct {
	Server string `json:"server"`
	Enable bool   `json:"enable"`
}

func (s *Server) fwdCtl(w http.ResponseWriter, r *http.Request) {
	var req fwdReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	if req.Enable {
		if req.Server == "" && s.Fwd.Server() == "" {
			writeErr(w, fmt.Errorf("server address required"))
			return
		}
		if req.Server != "" {
			if err := s.Fwd.SetServer(req.Server); err != nil {
				writeErr(w, err)
				return
			}
		}
		if !s.Fwd.Running() {
			if err := s.Fwd.Start(); err != nil {
				writeErr(w, err)
				return
			}
		}
	} else {
		s.Fwd.Stop()
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// healthz is a minimal liveness probe for the service.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ok := s.G.Connected() && s.G.Started()
	code := 200
	if !ok {
		code = 503
	}
	writeJSON(w, code, map[string]bool{"ok": ok})
}

// logs returns the tail of the in-memory daemon log ring.
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
 writeJSON(w, 200, map[string]any{"lines": ringWriter.Tail(100)})
}

// metrics exposes the concentrator + forwarder state in Prometheus text
// format so lampadas' prometheus (and thus the fleet dashboards) can scrape
// it like any node/exporter target.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	rxTotal, rxOK, rxBad, recoveries, errStreak := s.G.Counters()
	d, diagErr := s.G.Diag()
	trig, _ := s.G.TrigCount()
	txs, _ := s.G.TxStatus()

	gauge := func(name, help, tags string, v float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s%s %v\n", name, help, name, name, tags, v)
	}
	ctr := func(name, help, tags string, v float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n%s%s %v\n", name, help, name, name, tags, v)
	}

	gauge("lora8_connected", "1 if the USB bridge to the concentrator is open.", "", b2f(s.G.Connected()))
	gauge("lora8_started", "1 if the concentrator is configured and running.", "", b2f(s.G.Started()))
	gauge("lora8_txstatus", "TX modem state (1=off 2=free 3=scheduled 4=emitting).", "", float64(txs))
	gauge("lora8_rx_error_streak", "Consecutive RX bridge errors (self-heal triggers at 25).", "", float64(errStreak))
	ctr("lora8_recoveries_total", "Full auto-reconnect/restart cycles completed by the self-heal.", "", float64(recoveries))
	txRetries, txFail := s.G.TxCounters()
	ctr("lora8_tx_retries_total", "TX attempts that needed a self-heal retry.", "", float64(txRetries))
	ctr("lora8_tx_failures_total", "TX attempts that failed even after the retry.", "", float64(txFail))
	ctr("lora8_rx_packets_total", "Packets demodulated by the concentrator (any CRC status).", "", float64(rxTotal))
	ctr("lora8_rx_ok_total", "Received packets with OK/no-CRC status.", "", float64(rxOK))
	ctr("lora8_rx_bad_total", "Received packets with bad CRC.", "", float64(rxBad))
	gauge("lora8_trigcnt_us", "Concentrator internal microsecond counter.", "", float64(trig))
	gauge("lora8_fwd_running", "1 if the Semtech UDP forwarder is active.", "", b2f(s.Fwd.Running()))
	gauge("lora8_fwd_linked", "1 if an LNS ACK was seen in the last 30 s.", "", b2f(s.Fwd.Connected()))
	if la := s.Fwd.LastAck(); !la.IsZero() {
		gauge("lora8_fwd_last_ack_age_seconds", "Age of the last LNS ACK.", "", time.Since(la).Seconds())
	}
	ctr("lora8_fwd_rx_total", "Packets seen by the forwarder.", "", float64(s.Fwd.Stats.RxNb))
	ctr("lora8_fwd_rxok_total", "CRC-OK packets seen by the forwarder.", "", float64(s.Fwd.Stats.RxOK))
	ctr("lora8_fwd_rxfw_total", "Packets PUSHed upstream.", "", float64(s.Fwd.Stats.RxFw))
	ctr("lora8_fwd_downlinks_total", "Downlink commands received from the LNS.", "", float64(s.Fwd.Stats.DwNb))
	ctr("lora8_fwd_tx_total", "Packets transmitted via LNS command.", "", float64(s.Fwd.Stats.TxNb))
	if diagErr == nil && d != nil {
		gauge("lora8_fifo_depth", "Packets currently stored in the concentrator FIFO.", "", float64(d.FIFO))
		ctr("lora8_headers_total", "LoRa header detections (multi-SF).", `{chain="multisf"}`, float64(d.Hdr0))
		gauge("lora8_noise_rssi_raw", "Instantaneous noise/RSSI registers (raw units).", `{reg="chann"}`, float64(d.ChannRSSI))
		gauge("lora8_noise_rssi_raw", "Instantaneous noise/RSSI registers (raw units).", `{reg="bb"}`, float64(d.BBRSSI))
		gauge("lora8_noise_rssi_raw", "Instantaneous noise/RSSI registers (raw units).", `{reg="dec"}`, float64(d.DecRSSI))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Write([]byte(b.String()))
}
