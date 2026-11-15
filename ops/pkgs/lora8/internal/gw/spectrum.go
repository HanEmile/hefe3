// Spectral scan control: the SpectrumRunner schedules hal.SpectralScan
// sweeps against the gateway's single serial mutex.
//
// Design note (RX coexistence): spectrum mode and the RX poll loop share
// the one USB/SPI bridge, so they are mutually exclusive *during* a sweep:
// while a sweep runs (a few seconds for ~43 bins) the RX loop blocks on the
// gateway mutex and packets can be lost — the radio is detuned anyway (the
// sweep works by hopping the SX125x PLL). The C reference (util_spectral_
// scan / loragw_radio.c) relies on the same property: only the radio PLL is
// reprogrammed, the SX1301 demodulator state (IF config, modems, AGC) is
// left alone and the radio is retuned back at the end, so the RX loop
// resumes right after the sweep without a restart. Sweeping is off by
// default and only runs while "spectrum mode" is enabled (SetEnabled).
package gw

import (
	"log"
	"sync"
	"time"

	"github.com/HanEmile/lorahal/hal"
)

// SpectrumData is a snapshot of one completed spectral sweep.
type SpectrumData struct {
	Time    time.Time
	RFChain uint8
	Points  []hal.SpectralPoint
	Err     string // set when the sweep could not run
}

// SpectrumRunner runs a full spectral sweep every Interval while enabled,
// serialized against all other concentrator access. Created by New and
// reachable as Gateway.Spectrum.
type SpectrumRunner struct {
	g *Gateway

	mu      sync.Mutex
	enabled bool
	stopCh  chan struct{}
	latest  *SpectrumData
	sweeps  uint64

	bins     []uint32
	rfChain  uint8
	interval time.Duration
}

func newSpectrumRunner(g *Gateway) *SpectrumRunner {
	return &SpectrumRunner{
		g:        g,
		bins:     DefaultSpectrumBins(),
		rfChain:  0,
		interval: 30 * time.Second,
	}
}

// DefaultSpectrumBins returns the default sweep grid: 862.0-870.4 MHz in
// 200 kHz steps (43 bins), matching the classic util_spectral_scan defaults
// (863:0.2:870) extended to the full EU863-870 band.
func DefaultSpectrumBins() []uint32 {
	const start, stop, step = 862000000, 870400000, 200000
	bins := make([]uint32, 0, (stop-start)/step+1)
	for f := uint32(start); f <= stop; f += step {
		bins = append(bins, f)
	}
	return bins
}

// SetEnabled turns periodic sweeping on or off. Enabling schedules the
// first sweep immediately in its own goroutine; disabling cancels the loop
// (a sweep already holding the gateway mutex still runs to completion).
func (r *SpectrumRunner) SetEnabled(on bool) {
	r.mu.Lock()
	if r.enabled == on {
		r.mu.Unlock()
		return
	}
	r.enabled = on
	if on {
		stop := make(chan struct{})
		r.stopCh = stop
		r.mu.Unlock()
		go r.loop(stop)
	} else {
		close(r.stopCh)
		r.stopCh = nil
		r.mu.Unlock()
	}
}

// Enabled reports whether periodic sweeping is active.
func (r *SpectrumRunner) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

// Latest returns the most recent sweep result, or nil if none ran yet.
func (r *SpectrumRunner) Latest() *SpectrumData {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest
}

// Sweeps returns the number of sweep attempts (successful or not).
func (r *SpectrumRunner) Sweeps() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sweeps
}

func (r *SpectrumRunner) loop(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		r.sweepOnce()
		r.mu.Lock()
		iv := r.interval
		r.mu.Unlock()
		t := time.NewTimer(iv)
		select {
		case <-stop:
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// sweepOnce runs one full sweep under the gateway mutex. Failures are
// recorded in the snapshot (Err), not retried inline.
func (r *SpectrumRunner) sweepOnce() {
	g := r.g
	r.mu.Lock()
	rf, bins := r.rfChain, append([]uint32(nil), r.bins...)
	r.mu.Unlock()
	res := &SpectrumData{Time: time.Now(), RFChain: rf}
	g.mu.Lock()
	switch {
	case g.H == nil || !g.H.Started():
		res.Err = "concentrator not started"
	default:
		pts, err := g.H.SpectralScan(rf, bins)
		if err != nil {
			res.Err = err.Error()
		} else {
			res.Points = pts
		}
	}
	g.mu.Unlock()
	r.mu.Lock()
	r.latest = res
	r.sweeps++
	r.mu.Unlock()
	if res.Err != "" {
		log.Printf("gw: spectrum sweep failed: %s", res.Err)
	}
}
