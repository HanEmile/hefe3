// Spectral scan ("waterfall lite"): RSSI-per-frequency sweep using the
// SX125x RX PLL of a running concentrator.
//
// Reference research notes
// ------------------------
// The classic Semtech reference (Lora-net/lora_gateway util_spectral_scan)
// targets the *FPGA* reference design (SX1301AP2 + extra SX127x): the FPGA
// streams SX127x RSSI samples into a histogram RAM, the host just sets the
// scan frequency (freq<<19/32MHz into LGW_FPGA_HISTO_SCAN_FREQ) and reads
// back 128 RSSI bins of 1/2 dB resolution offset by -135 dBm. The R11e-LR8
// (and any plain SX1301+2x SX125x board) has neither FPGA nor SX127x, so
// that path cannot be ported 1:1.
//
// SX125x register 0x12 is IISM (rx/tx stream disable), NOT an RSSI register
// (SX1255/SX1257 datasheet, rev 3.x). The SX1255/SX1257 have no digital
// RSSI output at all — RSSI lives one level up in the SX1301 baseband, which
// continuously measures the two radio inputs. The SX1301 (picoGW reg map)
// exposes, on page 2 addr 60-62, the live AGC inputs CHANN_RSSI / BB_RSSI /
// DEC_RSSI of the IF chain selected by CHANN_SELECT_RSSI (page 0 addr 107).
//
// So the hardware-equivalent of the reference algorithm is:
//
//	for each frequency bin: halt the SX125x RX path, retune its PLL, wait
//	for lock, then repeatedly sample the SX1301 CHANN_RSSI register of a
//	125 kHz IF chain attached to that radio and keep the maximum.
//
// The radio is returned to its configured RX frequency afterwards, exactly
// like the C util restarts/reconfigures the radios on exit. No SX1301 IF
// configuration is modified — only the radio PLL frequency and the
// CHANN_SELECT_RSSI readout mux (both restored on return).
package hal

import (
	"errors"
	"fmt"
)

// SpectralPoint is one frequency bin of a spectral sweep.
type SpectralPoint struct {
	FreqHz uint32  // center frequency of the sampled 125 kHz passband
	RssiDb float32 // max of the CHANN_RSSI samples, converted to dBm
}

const (
	// per-bin sampling: settle after PLL lock, then take the max of N reads
	spectralSettleMs      = 8
	spectralSamplesPerBin = 5
	spectralSampleGapMs   = 3

	// RSSI_MULTI_BIAS from loragw_hal.c: difference between the multi-SF
	// channel RSSI offset and the stand-alone modem RSSI offset.
	spectralMultiBias = 35.0

	// SX125x practical RF tuning range
	spectralMinFreq = 800e6
	spectralMaxFreq = 1000e6
)

// retuneSX125x moves the RX PLL of one radio to freqHz, following the same
// sequence as setupSX125x: program the Frf registers, halt then re-enable
// the RX path (mode 0x00: 1 = XO on, 3 = XO+RX(PLL+FE) on) and poll the
// rxpll_locked bit (reg 0x11 bit 1) a few times.
func (h *Hal) retuneSX125x(rfChain uint8, freqHz uint32) error {
	var partInt, partFrac uint32
	switch h.rfRadioType[rfChain] {
	case RadioTypeSX1255:
		partInt = freqHz / (sx125x32MHzFrac << 7)
		partFrac = ((freqHz % (sx125x32MHzFrac << 7)) << 9) / sx125x32MHzFrac
	case RadioTypeSX1257:
		partInt = freqHz / (sx125x32MHzFrac << 8)
		partFrac = ((freqHz % (sx125x32MHzFrac << 8)) << 8) / sx125x32MHzFrac
	default:
		return fmt.Errorf("unexpected radio type %d", h.rfRadioType[rfChain])
	}
	for _, w := range [][2]uint8{
		{0x01, uint8(partInt)},
		{0x02, uint8(partFrac >> 8)},
		{0x03, uint8(partFrac)},
	} {
		if err := h.sx125xWrite(rfChain, w[0], w[1]); err != nil {
			return err
		}
	}
	for attempt := 1; ; attempt++ {
		// band-edge bins (PMR domains around 862 MHz) lock significantly
		// slower than mid-band; give them time instead of failing the bin.
		if attempt > 3*pllLockMaxAttempts {
			return fmt.Errorf("sx125x %d: fail to lock PLL after %d attempts", rfChain, 3*pllLockMaxAttempts)
		}
		if err := h.sx125xWrite(rfChain, 0x00, 1); err != nil {
			return err
		}
		if err := h.sx125xWrite(rfChain, 0x00, 3); err != nil {
			return err
		}
		waitMs(1 + attempt)
		v, err := h.sx125xRead(rfChain, 0x11)
		if err != nil {
			return err
		}
		if v&0x02 != 0 {
			return nil
		}
	}
}

// SpectralScan sweeps one radio across bins (absolute Hz) and returns the
// strongest measured RSSI per bin. h must be started and rfChain enabled;
// exactly one enabled IF chain on that radio is required as the RSSI
// readback window (the first match is used).
//
// Each bin tunes the radio to bin-ifFreq(readback chain), so the returned
// FreqHz is the true passband center. Bins whose implied radio frequency
// falls outside 800-1000 MHz are skipped silently.
//
// RssiDb follows the packet-RSSI convention of loragw_hal.c:
// raw CHANN_RSSI + board rssi_offset[rfChain], minus RSSI_MULTI_BIAS (35 dB)
// when the readback chain is a multi-SF one (0-7). Absolute accuracy is
// limited by the board offset; treat values primarily as relative.
//
// NOTE on RX continuity: hopping the radio PLL detunes every IF chain on
// that radio for the duration of the sweep — packets arriving then are
// lost. The demodulator state (IF frequencies, modems, AGC) is untouched
// and the radio is retuned back before returning, so normal reception
// resumes right after.
func (h *Hal) SpectralScan(rfChain uint8, bins []uint32) ([]SpectralPoint, error) {
	if rfChain >= RFChainNb {
		return nil, fmt.Errorf("invalid rf_chain %d", rfChain)
	}
	if !h.started {
		return nil, errors.New("spectral scan requires a started concentrator")
	}
	if !h.rfEnable[rfChain] {
		return nil, fmt.Errorf("rf_chain %d is disabled", rfChain)
	}
	ch := -1
	for i := 0; i < IFChainNb; i++ {
		if h.ifEnable[i] && h.ifRFChain[i] == rfChain {
			ch = i
			break
		}
	}
	if ch < 0 {
		return nil, fmt.Errorf("no enabled if chain on rf_chain %d", rfChain)
	}
	ifFreq := h.ifFreq[ch]

	oldSelect, err := h.RegR(LGW_CHANN_SELECT_RSSI)
	if err != nil {
		return nil, err
	}
	restoreFreq := h.rfRxFreq[rfChain]

	// The AGC MCU owns the radio SPI bus while the concentrator runs (the
	// start sequence clears FORCE_HOST_RADIO_CTRL). Host-side radio writes
	// like the sweeps below silently lose against the MCU — e.g. mid-speed
	// polarization glitches leading to PLL lock failures. Take the bus back
	// for the scan duration and hand it back on return.
	if err := h.RegW(LGW_FORCE_HOST_RADIO_CTRL, 1); err != nil {
		return nil, err
	}
	if err := h.RegW(LGW_FORCE_HOST_FE_CTRL, 1); err != nil {
		return nil, err
	}
	defer func() {
		h.RegW(LGW_FORCE_HOST_RADIO_CTRL, 0)
		h.RegW(LGW_FORCE_HOST_FE_CTRL, 0)
		h.RegW(LGW_CHANN_SELECT_RSSI, oldSelect)
		// best effort: radio back to its configured RX center
		if restoreFreq != 0 {
			h.retuneSX125x(rfChain, restoreFreq)
		}
	}()

	if err := h.RegW(LGW_CHANN_SELECT_RSSI, int32(ch)); err != nil {
		return nil, err
	}

	out := make([]SpectralPoint, 0, len(bins))
	for _, bin := range bins {
		if bin == 0 {
			continue
		}
		tune := int64(bin) - int64(ifFreq)
		if tune < spectralMinFreq || tune > spectralMaxFreq {
			continue
		}
		if err := h.retuneSX125x(rfChain, uint32(tune)); err != nil {
			return nil, fmt.Errorf("retune to %d Hz: %w", tune, err)
		}
		waitMs(spectralSettleMs)
		var maxRaw int32
		for s := 0; s < spectralSamplesPerBin; s++ {
			v, err := h.RegR(LGW_CHANN_RSSI)
			if err != nil {
				return nil, err
			}
			if s == 0 || v > maxRaw {
				maxRaw = v
			}
			if s < spectralSamplesPerBin-1 {
				waitMs(spectralSampleGapMs)
			}
		}
		rssi := float32(maxRaw) + h.rfRssiOffset[rfChain]
		if ch < 8 {
			rssi -= spectralMultiBias
		}
		out = append(out, SpectralPoint{FreqHz: bin, RssiDb: rssi})
	}
	return out, nil
}
