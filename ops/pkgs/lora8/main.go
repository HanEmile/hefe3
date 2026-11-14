// lora8 — tooling for the MikroTik R11e-LR8 (SX1301 LoRa concentrator with
// onboard USB<->SPI bridge), incl. a minimal web UI and a Semtech UDP
// packet forwarder.
//
// Subcommands:
//
//	lora8 info                          card detection + versions
//	lora8 listen                        configure EU868 + print received packets
//	lora8 send -freq 868.1 -payload hex one-shot TX
//	lora8 fwd -server host:port         listen-mode + Semtech UDP forwarder
//	lora8 web -addr :8080               web UI (everything)
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"emile.space/lora8/internal/fwd"
	"emile.space/lora8/internal/gw"
	"github.com/HanEmile/lorahal/hal"
	"emile.space/lora8/internal/web"
)

var devFlag = flag.String("dev", "/dev/ttyACM0", "serial device of the concentrator")

var startedAt = time.Now()

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[lora8] ")
	log.SetOutput(web.LogWriter(os.Stderr)) // tee into the /api/logs ring without changing callers
	flag.Parse()
	cmd := "web"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	args := flag.Args()[max(0, len(flag.Args())-flag.NArg()):]
	if flag.NArg() > 0 {
		args = flag.Args()[1:]
	}

	switch cmd {
	case "info":
		cmdInfo()
	case "probe":
		cmdProbe()
	case "listen":
		cmdListen()
	case "send":
		cmdSend(fs, args)
	case "fwd":
		cmdFwd(fs, args)
	case "web":
		cmdWeb(fs, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (info|listen|send|fwd|web)\n", cmd)
		os.Exit(2)
	}
}

func waitSig() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

func devPath() string { return *devFlag }

func cmdInfo() {
	h, err := hal.Connect(devPath())
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer h.Close(false)
	fmt.Printf("device:            %s\n", devPath())
	fmt.Printf("MCU bridge uid:    %s\n", hex.EncodeToString(h.MCUUid[:]))
	fmt.Printf("bridge fw offered: 0x%08X\n", h.MCUFWUsed)
	fmt.Printf("SX1301 chip ver:   v%d\n", h.SX1301Ver)
}

func configureAndStart(g *gw.Gateway, cfg gw.Config) {
	if err := g.ApplyConfigAndStart(cfg); err != nil {
		g.Disconnect(false)
		log.Fatalf("start: %v", err)
	}
	fmt.Println("concentrator started (EU868, TX on RF0)")
}

// cmdProbe starts the concentrator and streams raw diagnostig counters
// (RX FIFO fill, header/packet counters, RSSI registers, timestamp) so we can
// tell "RF-side silence" apart from "driver-side silence".
func cmdProbe() {
	g := gw.New(10, "")
	if err := g.Connect(devPath()); err != nil {
		log.Fatalf("connect: %v", err)
	}
	configureAndStart(g, gw.DefaultEU868())
	fmt.Println("timestamp      fifo  hdr0 pkt0 hdrMB pktMB hdrFSK pktFSK  channRSSI bbRSSI decRSSI")
	for i := 0; i < 120; i++ {
		d, err := g.Diag()
		if err != nil {
			log.Fatalf("diag: %v", err)
		}
		fmt.Printf("%14d %5d %4d %4d %5d %5d %6d %6d %9d %6d %7d\n",
			d.Timestamp, d.FIFO, d.Hdr0, d.Pkt0, d.HdrMB, d.PktMB, d.HdrFSK, d.PktFSK, d.ChannRSSI, d.BBRSSI, d.DecRSSI)
		time.Sleep(1 * time.Second)
	}
	g.Disconnect(false)
}

func cmdListen() {
	g := gw.New(500, "")
	if err := g.Connect(devPath()); err != nil {
		log.Fatalf("connect: %v", err)
	}
	uid, fw, chip, _ := g.Info()
	fmt.Printf("connected: mcu=%s bridge_fw=0x%08X chip=v%d\n", hex.EncodeToString(uid[:]), fw, chip)
	configureAndStart(g, gw.DefaultEU868())
	g.Subscribe(func(pkts []hal.RxPacket) {
		for _, p := range pkts {
			st := map[uint8]string{hal.StatCRCOK: "CRC-OK", hal.StatCRCBad: "CRC-BAD", hal.StatNoCRC: "NO-CRC"}[p.Status]
			fmt.Printf("%s  %8.3f MHz  if=%d rf=%d  SF%d BW%d CR 4/%d  rssi=%.1f snr=%.1f  %s  len=%d  %s\n",
				time.Now().Format("15:04:05.000"), float64(p.FreqHz)/1e6, p.IFChain, p.RFChain, dr2sf(p.Datarate), bwNum(p.Bandwidth), p.Coderate, p.Rssi, p.Snr, st, p.Size, hex.EncodeToString(p.Payload))
		}
	})
	fmt.Println("listening... (ctrl-c to stop)")
	waitSig()
	g.Disconnect(false)
}

func cmdSend(fs *flag.FlagSet, args []string) {
	freq := fs.Float64("freq", 868.1, "TX frequency in MHz")
	rf := fs.Int("rf", 0, "RF chain")
	sf := fs.Int("sf", 7, "spreading factor 7..12")
	bw := fs.Int("bw", 125, "bandwidth kHz: 125|250|500")
	cr := fs.Int("cr", 4, "coderate: 1=4/5 2=4/6 3=4/7 4=4/8")
	pow := fs.Int("power", 14, "TX power in dBm")
	pl := fs.String("payload", "", "payload (hex)")
	fs.Parse(args)
	payload, err := hex.DecodeString(*pl)
	if err != nil || len(payload) == 0 {
		log.Fatalf("payload must be non-empty hex")
	}
	g := gw.New(100, "")
	if err := g.Connect(devPath()); err != nil {
		log.Fatalf("connect: %v", err)
	}
	configureAndStart(g, gw.DefaultEU868())
	pkt := hal.TxPacket{
		FreqHz:     uint32(*freq * 1e6),
		TxMode:     hal.TxImmediate,
		RFChain:    uint8(*rf),
		RFPower:    int8(*pow),
		Modulation: hal.ModLora,
		Bandwidth:  map[int]uint8{125: hal.BW125kHz, 250: hal.BW250kHz, 500: hal.BW500kHz}[*bw],
		Datarate:   map[int]uint32{7: hal.DRLoraSF7, 8: hal.DRLoraSF8, 9: hal.DRLoraSF9, 10: hal.DRLoraSF10, 11: hal.DRLoraSF11, 12: hal.DRLoraSF12}[*sf],
		Coderate:   uint8(*cr),
		Payload:    payload,
	}
	toa := hal.TimeOnAir(pkt, 3)
	if err := g.Send(pkt); err != nil {
		g.Disconnect(false)
		log.Fatalf("send: %v", err)
	}
	fmt.Printf("sent %d bytes, airtime %d ms\n", len(payload), toa)
	waitTx(g, toa+2000)
	g.Disconnect(false)
}

func waitTx(g *gw.Gateway, maxMs uint32) {
	t0 := time.Now()
	for time.Since(t0) < time.Duration(maxMs)*time.Millisecond {
		st, err := g.TxStatus()
		if err == nil && (st == hal.TxFree) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func cmdFwd(fs *flag.FlagSet, args []string) {
	server := fs.String("server", "", "LNS address host:port (e.g. eu1.cloud.thethings.network:1700)")
	fs.Parse(args)
	g := gw.New(500, "")
	if err := g.Connect(devPath()); err != nil {
		log.Fatalf("connect: %v", err)
	}
	configureAndStart(g, gw.DefaultEU868())
	uid, _, _, _ := g.Info()
	f := &fwd.Forwarder{Send: g.Send}
	copy(f.EUI[:], uid[:])
	if err := f.SetServer(*server); err != nil {
		log.Fatalf("server: %v", err)
	}
	if *server == "" {
		log.Fatalf("need -server")
	}
	if err := f.Start(); err != nil {
		log.Fatalf("fwd: %v", err)
	}
	fmt.Printf("forwarding to %s (gateway EUI %s)\n", *server, strings.ToUpper(hex.EncodeToString(uid[:])))
	waitSig()
	f.Stop()
	g.Disconnect(false)
}

func cmdWeb(fs *flag.FlagSet, args []string) {
	addr := fs.String("addr", ":8080", "listen address")
	auto := fs.Bool("auto", false, "connect + configure(EU868) + start immediately")
	dev := fs.String("autodev", "", "override -dev for -auto")
	fwdSrv := fs.String("fwd", "", "enable Semtech UDP forwarder to this LNS host:port on startup")
	logDir := fs.String("logdir", "", "directory for persistent frame history (daily JSONL files)")
	fs.Parse(args)

	g := gw.New(1000, *logDir)
	f := &fwd.Forwarder{Send: g.Send}
	if *auto {
		p := devPath()
		if *dev != "" {
			p = *dev
		}
		go func() {
			// Retry until the bridge shows up (replug, udev symlink, MCU reset).
			var err error
			for err = g.Connect(p); err != nil; err = g.Connect(p) {
				log.Printf("auto-connect failed (%v), retrying in 10s", err)
				time.Sleep(10 * time.Second)
			}
			uid, _, _, _ := g.Info()
			copy(f.EUI[:], uid[:])
			if err := g.ApplyConfigAndStart(gw.DefaultEU868()); err != nil {
				log.Printf("auto-start failed: %v", err)
			} else {
				log.Printf("concentrator started on %s", p)
			}
			if *fwdSrv != "" {
				if err := f.SetServer(*fwdSrv); err != nil {
					log.Printf("bad fwd server: %v", err)
				} else if err := f.Start(); err != nil {
					log.Printf("fwd start failed: %v", err)
				} else {
					log.Printf("forwarding to %s (gateway EUI %s)", *fwdSrv, strings.ToUpper(hex.EncodeToString(uid[:])))
				}
			}
		}()
	}
	g.Subscribe(f.OnPackets)
	s := web.New(g, f, gw.DefaultEU868())
	s.StartedAt = startedAt
	srv := &http.Server{Addr: *addr, Handler: s.Handler()}
	fmt.Printf("web UI listening on http://%s\n", displayAddr(*addr))
	log.Fatal(srv.ListenAndServe())
}

func displayAddr(a string) string {
	if strings.HasPrefix(a, ":") {
		host, _ := os.Hostname()
		return host + a
	}
	return a
}

func dr2sf(dr uint32) int {
	m := map[uint32]int{hal.DRLoraSF7: 7, hal.DRLoraSF8: 8, hal.DRLoraSF9: 9, hal.DRLoraSF10: 10, hal.DRLoraSF11: 11, hal.DRLoraSF12: 12}
	return m[dr]
}

func bwNum(bw uint8) int {
	m := map[uint8]int{hal.BW500kHz: 500, hal.BW250kHz: 250, hal.BW125kHz: 125, hal.BW62k5Hz: 62, hal.BW31k2Hz: 31, hal.BW15k6Hz: 15, hal.BW7k8Hz: 7}
	return m[bw]
}
