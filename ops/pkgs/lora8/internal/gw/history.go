package gw

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/HanEmile/lorahal/hal"
)

// FrameRecord is the persistent, self-describing representation of one
// frame line in the JSONL history (one file per UTC day).
type FrameRecord struct {
	TS     int64   `json:"ts"` // unix ms
	Seq    uint64  `json:"seq"`
	Dir    string  `json:"dir"` // "rx" | "tx"
	FreqHz uint32  `json:"freq_hz,omitempty"`
	RF     uint8   `json:"rf,omitempty"`
	If     int     `json:"if,omitempty"`
	Mod    string  `json:"mod"` // LORA | FSK
	SF     int     `json:"sf,omitempty"`
	BW     int     `json:"bw,omitempty"` // kHz
	CR     int     `json:"cr,omitempty"`
	BR     uint32  `json:"br,omitempty"` // FSK baud
	Size   uint16  `json:"size,omitempty"`
	Hex    string  `json:"hex,omitempty"`
	RSSI   float32 `json:"rssi,omitempty"`
	SNR    float32 `json:"snr,omitempty"`
	CRC    string  `json:"crc,omitempty"`    // rx: ok | bad | nocrc
	Status string  `json:"status,omitempty"` // tx: ok | failed
	Err    string  `json:"err,omitempty"`
	ToaMs  uint32  `json:"toa_ms,omitempty"`
	Power  int8    `json:"power,omitempty"`
}

func recordFromEntry(e LogEntry) FrameRecord {
	r := FrameRecord{TS: e.Time.UnixMilli(), Seq: e.Seq, Dir: e.Dir}
	if e.Rx != nil {
		p := e.Rx
		r.FreqHz, r.RF, r.If = p.FreqHz, p.RFChain, int(p.IFChain)
		if p.Modulation == hal.ModLora {
			r.Mod = "LORA"
			r.SF = sfOfDR(p.Datarate)
			r.BW = khzOfBW(p.Bandwidth)
		} else {
			r.Mod = "FSK"
			r.BR = p.Datarate
		}
		r.CR = int(p.Coderate)
		r.RSSI, r.SNR = p.Rssi, p.Snr
		switch p.Status {
		case hal.StatCRCOK:
			r.CRC = "ok"
		case hal.StatCRCBad:
			r.CRC = "bad"
		case hal.StatNoCRC:
			r.CRC = "nocrc"
		default:
			r.CRC = fmt.Sprintf("0x%02x", p.Status)
		}
		r.Size, r.Hex = p.Size, hex.EncodeToString(p.Payload)
	}
	if e.Tx != nil {
		p := e.Tx
		r.FreqHz, r.RF, r.Power = p.FreqHz, p.RFChain, p.RFPower
		if p.Modulation == hal.ModLora {
			r.Mod = "LORA"
			r.SF = sfOfDR(p.Datarate)
			r.BW = khzOfBW(p.Bandwidth)
		} else {
			r.Mod = "FSK"
			r.BR = p.Datarate
		}
		r.CR = int(p.Coderate)
		r.Size, r.Hex = p.Size, hex.EncodeToString(p.Payload)
		r.ToaMs = e.TxToa
		if e.TxErr != "" {
			r.Status, r.Err = "failed", e.TxErr
		} else {
			r.Status = "ok"
		}
	}
	return r
}

func sfOfDR(dr uint32) int {
	return map[uint32]int{hal.DRLoraSF7: 7, hal.DRLoraSF8: 8, hal.DRLoraSF9: 9, hal.DRLoraSF10: 10, hal.DRLoraSF11: 11, hal.DRLoraSF12: 12}[dr]
}
func drOfSF(sf int) uint32 {
	return map[int]uint32{7: hal.DRLoraSF7, 8: hal.DRLoraSF8, 9: hal.DRLoraSF9, 10: hal.DRLoraSF10, 11: hal.DRLoraSF11, 12: hal.DRLoraSF12}[sf]
}
func khzOfBW(bw uint8) int {
	return map[uint8]int{hal.BW500kHz: 500, hal.BW250kHz: 250, hal.BW125kHz: 125, hal.BW62k5Hz: 62, hal.BW31k2Hz: 31, hal.BW15k6Hz: 15, hal.BW7k8Hz: 7}[bw]
}
func bwOfKhz(k int) uint8 {
	return map[int]uint8{500: hal.BW500kHz, 250: hal.BW250kHz, 125: hal.BW125kHz, 62: hal.BW62k5Hz, 31: hal.BW31k2Hz, 15: hal.BW15k6Hz, 7: hal.BW7k8Hz}[k]
}

func entryFromRecord(r FrameRecord) LogEntry {
	e := LogEntry{Seq: r.Seq, Dir: r.Dir, Time: time.UnixMilli(r.TS).Local()}
	payload, _ := hex.DecodeString(r.Hex)
	mod := uint8(hal.ModLora)
	dr := drOfSF(r.SF)
	bw := bwOfKhz(r.BW)
	if r.Mod == "FSK" {
		mod = hal.ModFsk
		dr = r.BR
		bw = 0
	}
	if r.Dir == "rx" {
		p := &hal.RxPacket{
			FreqHz: r.FreqHz, RFChain: r.RF, IFChain: uint8(r.If),
			Modulation: mod, Datarate: dr, Bandwidth: bw, Coderate: uint8(r.CR),
			Rssi: r.RSSI, Snr: r.SNR, Size: r.Size, Payload: payload,
		}
		switch r.CRC {
		case "ok":
			p.Status = hal.StatCRCOK
		case "bad":
			p.Status = hal.StatCRCBad
		case "nocrc":
			p.Status = hal.StatNoCRC
		}
		e.Rx = p
	} else {
		p := &hal.TxPacket{
			FreqHz: r.FreqHz, RFChain: r.RF, RFPower: r.Power,
			Modulation: mod, Datarate: dr, Bandwidth: bw, Coderate: uint8(r.CR),
			Size: r.Size, Payload: payload,
		}
		e.Tx = p
		e.TxToa = r.ToaMs
		e.TxErr = r.Err
	}
	return e
}

// HistoryWriter appends frames to a per-UTC-day JSONL file.
type HistoryWriter struct {
	dir string
	mu  sync.Mutex
	day string
	f   *os.File
}

// NewHistoryWriter creates dir if needed and returns a writer.
func NewHistoryWriter(dir string) (*HistoryWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &HistoryWriter{dir: dir}, nil
}

func (w *HistoryWriter) curFile() (*os.File, error) {
	today := time.Now().UTC().Format("2006-01-02")
	if w.day == today && w.f != nil {
		return w.f, nil
	}
	if w.f != nil {
		w.f.Close()
	}
	f, err := os.OpenFile(filepath.Join(w.dir, "frames-"+today+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	w.day, w.f = today, f
	return f, nil
}

// Append writes one frame record.
func (w *HistoryWriter) Append(r FrameRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := w.curFile()
	if err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Close flushes and closes the current file.
func (w *HistoryWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil {
		w.f.Close()
		w.f = nil
	}
}

// LoadRecent reads today's and yesterday's JSONL files and returns at most
// the last n entries plus the highest seq seen.
func LoadRecent(dir string, n int) (entries []LogEntry, lastSeq uint64, err error) {
	files := []string{
		filepath.Join(dir, "frames-"+time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")+".jsonl"),
		filepath.Join(dir, "frames-"+time.Now().UTC().Format("2006-01-02")+".jsonl"),
	}
	var recs []FrameRecord
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var r FrameRecord
			if json.Unmarshal(sc.Bytes(), &r) == nil {
				recs = append(recs, r)
			}
		}
		f.Close()
	}
	for _, r := range recs {
		if r.Seq > lastSeq {
			lastSeq = r.Seq
		}
	}
	if len(recs) > n {
		recs = recs[len(recs)-n:]
	}
	for _, r := range recs {
		entries = append(entries, entryFromRecord(r))
	}
	return entries, lastSeq, nil
}
