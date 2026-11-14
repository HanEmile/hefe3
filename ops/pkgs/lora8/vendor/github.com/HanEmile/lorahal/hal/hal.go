// Package hal is a Go port of the host-side of the Semtech PicoCell GW HAL
// (Lora-net/picoGW_hal: loragw_hal.c, loragw_mcu.c, loragw_radio.c,
// loragw_reg.c), driving an SX1301 LoRa concentrator through the STM32
// USB<->SPI bridge found on the MikroTik R11e-LR8.
package hal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/HanEmile/lorahal/bridge"
)

// Hardware constants (from loragw_hal.h)
const (
	XtalFreq    = 32000000
	RFChainNb   = 2
	IFChainNb   = 10
	PktFIFOSize = 16
	MultiNb     = 8
	RefBW       = 125000

	IFUndefined = 0
	IFLoraStd   = 0x10
	IFLoraMulti = 0x11
	IFFskStd    = 0x20

	ModUndefined = 0
	ModLora      = 0x10
	ModFsk       = 0x20

	BWUndefined = 0
	BW500kHz    = 0x01
	BW250kHz    = 0x02
	BW125kHz    = 0x03
	BW62k5Hz    = 0x04
	BW31k2Hz    = 0x05
	BW15k6Hz    = 0x06
	BW7k8Hz     = 0x07

	DRUndefined = 0
	DRLoraSF7   = 0x02
	DRLoraSF8   = 0x04
	DRLoraSF9   = 0x08
	DRLoraSF10  = 0x10
	DRLoraSF11  = 0x20
	DRLoraSF12  = 0x40
	DRLoraMulti = 0x7E
	DRFSKMin    = 500
	DRFSKMax    = 250000

	CRUndefined = 0
	CRLora45    = 0x01
	CRLora46    = 0x02
	CRLora47    = 0x03
	CRLora48    = 0x04

	StatUndefined = 0x00
	StatNoCRC     = 0x01
	StatCRCBad    = 0x11
	StatCRCOK     = 0x10

	TxImmediate   = 0
	TxTimestamped = 1
	TxOnGPS       = 2

	TxStatusUnknown  = 0
	TxOff            = 1
	TxFree           = 2
	TxScheduled      = 3
	TxEmitting       = 4
	TxGainLUTSizeMax = 16

	RadioTypeNone   = 0
	RadioTypeSX1255 = 1
	RadioTypeSX1257 = 2

	mcuARB = 0
	mcuAGC = 1

	mcuFwByte     = 8192
	fwVersionAddr = 0x20
	fwVersionCal  = 2
	fwVersionAGC  = 4
	fwVersionARB  = 1

	agcCmdWait  = 16
	agcCmdAbort = 17

	txStartDelay = 1500

	// STM32 bridge firmware versions we offer to the MCU ('l' command);
	// the first ACK_OK wins. MikroTik shipped at least 0x010a0004.
	// Ordered newest first.
)

var mcuFWVersions = []uint32{0x010a0006, 0x010a0005, 0x010a0004, 0x010a0003, 0x010a0002, 0x010a0001, 0x010a0000}

var ifModemConfig = [IFChainNb]int{
	IFLoraMulti, IFLoraMulti, IFLoraMulti, IFLoraMulti,
	IFLoraMulti, IFLoraMulti, IFLoraMulti, IFLoraMulti,
	IFLoraStd, IFFskStd,
}

// BoardConf is the board-level configuration.
type BoardConf struct {
	LorawanPublic bool
	ClkSrc        uint8
}

// RxRFConf configures one RF chain (radio).
type RxRFConf struct {
	Enable     bool
	FreqHz     uint32
	RssiOffset float32
	Type       int
	TxEnable   bool
}

// RxIFConf configures one IF chain + modem.
type RxIFConf struct {
	Enable       bool
	RFChain      uint8
	FreqHz       int32
	Bandwidth    uint8
	Datarate     uint32
	SyncWordSize uint8
	SyncWord     uint64
}

// TxGain is one entry of the TX gain LUT.
type TxGain struct {
	DigGain uint8
	PaGain  uint8
	DacGain uint8
	MixGain uint8
	RFPower int8
}

// TxGainLUT is the TX gain lookup table.
type TxGainLUT struct {
	LUT  [TxGainLUTSizeMax]TxGain
	Size int
}

// RxPacket is one received packet with metadata.
type RxPacket struct {
	FreqHz     uint32
	IFChain    uint8
	Status     uint8
	CountUs    uint32
	RFChain    uint8
	Modulation uint8
	Bandwidth  uint8
	Datarate   uint32
	Coderate   uint8
	Rssi       float32
	Snr        float32
	SnrMin     float32
	SnrMax     float32
	Crc        uint16
	Size       uint16
	Payload    []byte
}

// TxPacket describes a packet to transmit.
type TxPacket struct {
	FreqHz     uint32
	TxMode     uint8
	CountUs    uint32
	RFChain    uint8
	RFPower    int8
	Modulation uint8
	Bandwidth  uint8
	Datarate   uint32
	Coderate   uint8
	InvertPol  bool
	FDev       uint8
	Preamble   uint16
	NoCRC      bool
	NoHeader   bool
	Size       uint16
	Payload    []byte
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// Hal drives one concentrator.
type Hal struct {
	c         *bridge.Bridge
	started   bool
	MCUUid    [8]byte
	MCUFWUsed uint32
	SX1301Ver int

	lorawanPublic bool
	rfClkout      uint8

	rfEnable     [RFChainNb]bool
	rfRxFreq     [RFChainNb]uint32
	rfRssiOffset [RFChainNb]float32
	rfTxEnable   [RFChainNb]bool
	rfRadioType  [RFChainNb]int

	ifEnable  [IFChainNb]bool
	ifRFChain [IFChainNb]uint8
	ifFreq    [IFChainNb]int32
	sfmask    [MultiNb]uint8

	loraRxBW        uint8
	loraRxSF        uint8
	loraRxPPMOffset bool

	fskRxBW         uint8
	fskRxDR         uint32
	fskSyncWordSize uint8
	fskSyncWord     uint64

	txGainLUT TxGainLUT

	calOffsetAI [8]int8
	calOffsetAQ [8]int8
	calOffsetBI [8]int8
	calOffsetBQ [8]int8
}

// DefaultTxGainLUT is the LUT shipped by picoGW_hal.
func DefaultTxGainLUT() TxGainLUT {
	return TxGainLUT{
		Size: 2,
		LUT: [TxGainLUTSizeMax]TxGain{
			{DigGain: 0, PaGain: 2, DacGain: 3, MixGain: 10, RFPower: 14},
			{DigGain: 0, PaGain: 3, DacGain: 3, MixGain: 14, RFPower: 27},
		},
	}
}

// Connect opens the bridge, checks MCU bridge firmware compatibility and the
// SX1301 chip version.
func Connect(path string) (*Hal, error) {
	br, err := bridge.Open(path)
	if err != nil {
		return nil, err
	}
	h := &Hal{c: br, txGainLUT: DefaultTxGainLUT(), fskSyncWordSize: 3, fskSyncWord: 0xC194C1}

	// MCU firmware version negotiation ('l')
	ok := false
	for _, fw := range mcuFWVersions {
		ans, err := br.Command('l', 0, []byte{byte(fw >> 24), byte(fw >> 16), byte(fw >> 8), byte(fw)})
		if err != nil {
			br.Close()
			return nil, fmt.Errorf("MCU fw version check: %w", err)
		}
		if ans.Status == 1 && len(ans.Data) >= 8 {
			copy(h.MCUUid[:], ans.Data[:8])
			h.MCUFWUsed = fw
			ok = true
			break
		}
	}
	if !ok {
		br.Close()
		return nil, errors.New("no compatible MCU bridge firmware version found")
	}

	// SX1301 version check
	v, err := br.R(loregs[LGW_VERSION].addr)
	if err != nil {
		br.Close()
		return nil, err
	}
	if int(v) != int(loregs[LGW_VERSION].dflt) {
		br.Close()
		return nil, fmt.Errorf("unexpected chip version %d (expected %d)", v, loregs[LGW_VERSION].dflt)
	}
	h.SX1301Ver = int(v)

	if err := br.W(loregs[LGW_PAGE_REG].addr, 0); err != nil {
		br.Close()
		return nil, err
	}
	return h, nil
}

// Close closes the bridge. If resetMCU is true the bridge MCU is reset first
// (it re-enumerates on USB afterwards).
func (h *Hal) Close(resetMCU bool) error {
	if h.c == nil {
		return nil
	}
	if resetMCU {
		h.c.Command('m', 0, nil)
	}
	c := h.c
	h.c = nil
	return c.Close()
}

// SoftReset resets the SX1301 (not the MCU).
func (h *Hal) SoftReset() error {
	return h.c.W(0, 0x80) // PAGE_REG addr with SOFT_RESET bit
}

func (h *Hal) pageSwitch(page int8) error {
	return h.c.W(0, uint8(page)&0x03)
}

// --- register access (port of reg_w_align32 / reg_r_align32) --------------

// RegW writes an SX1301 register by name.
func (h *Hal) RegW(id int, val int32) error {
	if id == LGW_PAGE_REG {
		return h.pageSwitch(int8(val))
	}
	if id == LGW_SOFT_RESET {
		if val&1 != 0 {
			return h.SoftReset()
		}
		return nil
	}
	r := loregs[id]
	if r.rdon {
		return fmt.Errorf("reg %d is read-only", id)
	}
	if r.page != -1 {
		if err := h.pageSwitch(r.page); err != nil {
			return err
		}
	}
	switch {
	case r.leng == 8 && r.offs == 0:
		return h.c.W(r.addr, uint8(val))
	case r.offs+r.leng <= 8:
		old, err := h.c.R(r.addr)
		if err != nil {
			return err
		}
		mask := uint8(((1 << r.leng) - 1) << r.offs)
		nv := old&^mask | (uint8(val)<<r.offs)&mask
		return h.c.W(r.addr, nv)
	case r.offs == 0 && r.leng <= 32:
		n := (int(r.leng) + 7) / 8
		buf := make([]byte, n)
		v := val
		for i := 0; i < n; i++ {
			buf[i] = byte(v)
			v >>= 8
		}
		return h.c.Wb(r.addr, buf)
	default:
		return fmt.Errorf("reg %d: unsupported size/offset", id)
	}
}

// RegR reads an SX1301 register by name.
func (h *Hal) RegR(id int) (int32, error) {
	r := loregs[id]
	if r.page != -1 {
		if err := h.pageSwitch(r.page); err != nil {
			return 0, err
		}
	}
	switch {
	case r.offs+r.leng <= 8:
		v, err := h.c.R(r.addr)
		if err != nil {
			return 0, err
		}
		if r.sign {
			s := int8(v << (8 - r.leng - r.offs)) // left-align in int8
			return int32(s >> (8 - r.leng)), nil  // arithmetic right shift
		}
		return int32((v << (8 - r.leng - r.offs)) >> (8 - r.leng)), nil
	case r.offs == 0 && r.leng <= 32:
		n := (int(r.leng) + 7) / 8
		buf, err := h.c.Rb(r.addr, n)
		if err != nil {
			return 0, err
		}
		var u uint32
		for i := n - 1; i >= 0; i-- {
			u = u<<8 | uint32(buf[i])
		}
		if r.sign {
			s := int32(u << (32 - uint32(r.leng)))
			return s >> (32 - r.leng), nil
		}
		return int32(u), nil
	default:
		return 0, fmt.Errorf("reg %d: unsupported size/offset", id)
	}
}

// RegWb does a burst write starting at a register address.
func (h *Hal) RegWb(id int, data []byte) error {
	r := loregs[id]
	if r.rdon {
		return fmt.Errorf("reg %d is read-only", id)
	}
	if r.page != -1 {
		if err := h.pageSwitch(r.page); err != nil {
			return err
		}
	}
	return h.c.Wb(r.addr, data)
}

// RegRb does a burst read starting at a register address.
func (h *Hal) RegRb(id int, size int) ([]byte, error) {
	r := loregs[id]
	if r.page != -1 {
		if err := h.pageSwitch(r.page); err != nil {
			return nil, err
		}
	}
	return h.c.Rb(r.addr, size)
}

func waitMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// --- SX125x radio control (port of loragw_radio.c) -------------------------

func (h *Hal) sx125xWrite(channel uint8, addr, data uint8) error {
	var regAdd, regDat, regCs int
	switch channel {
	case 0:
		regAdd, regDat, regCs = LGW_SPI_RADIO_A__ADDR, LGW_SPI_RADIO_A__DATA, LGW_SPI_RADIO_A__CS
	case 1:
		regAdd, regDat, regCs = LGW_SPI_RADIO_B__ADDR, LGW_SPI_RADIO_B__DATA, LGW_SPI_RADIO_B__CS
	default:
		return fmt.Errorf("invalid rf chain %d", channel)
	}
	if addr >= 0x7F {
		return fmt.Errorf("sx125x address out of range: %d", addr)
	}
	for _, op := range [][2]int32{
		{int32(regCs), 0}, {int32(regAdd), int32(0x80 | addr)}, {int32(regDat), int32(data)}, {int32(regCs), 1}, {int32(regCs), 0},
	} {
		if err := h.RegW(int(op[0]), op[1]); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hal) sx125xRead(channel uint8, addr uint8) (uint8, error) {
	var regAdd, regDat, regCs, regRb int
	switch channel {
	case 0:
		regAdd, regDat, regCs, regRb = LGW_SPI_RADIO_A__ADDR, LGW_SPI_RADIO_A__DATA, LGW_SPI_RADIO_A__CS, LGW_SPI_RADIO_A__DATA_READBACK
	case 1:
		regAdd, regDat, regCs, regRb = LGW_SPI_RADIO_B__ADDR, LGW_SPI_RADIO_B__DATA, LGW_SPI_RADIO_B__CS, LGW_SPI_RADIO_B__DATA_READBACK
	default:
		return 0, fmt.Errorf("invalid rf chain %d", channel)
	}
	for _, op := range [][2]int32{
		{int32(regCs), 0}, {int32(regAdd), int32(addr)}, {int32(regDat), 0}, {int32(regCs), 1}, {int32(regCs), 0},
	} {
		if err := h.RegW(int(op[0]), op[1]); err != nil {
			return 0, err
		}
	}
	v, err := h.RegR(regRb)
	return uint8(v), err
}

// SX125x default trims (loragw_sx125x.h)
const (
	sx125xTxDacClkSel  = 1
	sx125xTxDacGain    = 2
	sx125xTxMixGain    = 14
	sx125xTxPllBw      = 1
	sx125xTxAnaBw      = 0
	sx125xTxDacBw      = 5
	sx125xRxLnaGain    = 1
	sx125xRxBbGain     = 12
	sx125xLnaZin       = 1
	sx125xRxAdcBw      = 7
	sx125xRxAdcTrim    = 6
	sx125xRxBbBw       = 0
	sx125xRxPllBw      = 0
	sx125xAdcTemp      = 0
	sx125xXoscGmStart  = 13
	sx125xXoscDisable  = 2
	sx125x32MHzFrac    = 15625
	pllLockMaxAttempts = 5
)

func (h *Hal) setupSX125x(rfChain uint8, clkout bool, enable bool, radioType int, freqHz uint32) error {
	if _, err := h.sx125xRead(rfChain, 0x07); err != nil {
		return err
	}
	clkReg := sx125xTxDacClkSel
	if clkout {
		clkReg += 2
	}
	if err := h.sx125xWrite(rfChain, 0x10, uint8(clkReg)); err != nil {
		return err
	}
	switch radioType {
	case RadioTypeSX1255:
		if err := h.sx125xWrite(rfChain, 0x28, sx125xXoscGmStart+sx125xXoscDisable*16); err != nil {
			return err
		}
	case RadioTypeSX1257:
		if err := h.sx125xWrite(rfChain, 0x26, sx125xXoscGmStart+sx125xXoscDisable*16); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected radio type %d", radioType)
	}
	if !enable {
		return nil
	}
	for _, w := range [][2]uint8{
		{0x08, sx125xTxMixGain + sx125xTxDacGain*16},
		{0x0A, sx125xTxAnaBw + sx125xTxPllBw*32},
		{0x0B, sx125xTxDacBw},
		{0x0C, sx125xLnaZin + sx125xRxBbGain*2 + sx125xRxLnaGain*32},
		{0x0D, sx125xRxBbBw + sx125xRxAdcTrim*4 + sx125xRxAdcBw*32},
		{0x0E, sx125xAdcTemp + sx125xRxPllBw*2},
	} {
		if err := h.sx125xWrite(rfChain, w[0], w[1]); err != nil {
			return err
		}
	}
	var partInt, partFrac uint32
	switch radioType {
	case RadioTypeSX1255:
		partInt = freqHz / (sx125x32MHzFrac << 7)
		partFrac = ((freqHz % (sx125x32MHzFrac << 7)) << 9) / sx125x32MHzFrac
	case RadioTypeSX1257:
		partInt = freqHz / (sx125x32MHzFrac << 8)
		partFrac = ((freqHz % (sx125x32MHzFrac << 8)) << 8) / sx125x32MHzFrac
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
		if attempt > pllLockMaxAttempts {
			return fmt.Errorf("sx125x %d: fail to lock PLL", rfChain)
		}
		if err := h.sx125xWrite(rfChain, 0x00, 1); err != nil {
			return err
		}
		if err := h.sx125xWrite(rfChain, 0x00, 3); err != nil {
			return err
		}
		waitMs(1)
		v, err := h.sx125xRead(rfChain, 0x11)
		if err != nil {
			return err
		}
		if v&0x02 != 0 {
			return nil
		}
	}
}

// --- MCU firmware loading (port of load_firmware) --------------------------

func (h *Hal) loadFirmware(target int, fw []byte) error {
	if len(fw) != mcuFwByte {
		return fmt.Errorf("invalid firmware size %d", len(fw))
	}
	var regRst, regSel int
	if target == mcuARB {
		regRst, regSel = LGW_MCU_RST_0, LGW_MCU_SELECT_MUX_0
	} else {
		regRst, regSel = LGW_MCU_RST_1, LGW_MCU_SELECT_MUX_1
	}
	if err := h.RegW(regRst, 1); err != nil {
		return err
	}
	if err := h.RegW(regSel, 0); err != nil {
		return err
	}
	if err := h.RegW(LGW_MCU_PROM_ADDR, 0); err != nil {
		return err
	}
	if err := h.RegWb(LGW_MCU_PROM_DATA, fw); err != nil {
		return err
	}
	if _, err := h.RegR(LGW_MCU_PROM_DATA); err != nil { // bug workaround
		return err
	}
	check, err := h.RegRb(LGW_MCU_PROM_DATA, len(fw))
	if err != nil {
		return err
	}
	if !bytes.Equal(fw, check) {
		return errors.New("firmware readback mismatch")
	}
	return h.RegW(regSel, 1)
}

// --- calibration (port of lgw_calibrate_sx125x) ----------------------------

func (h *Hal) calibrateSX125x(calFW []byte, idxStart, idxNb int) error {
	if idxStart < 5 || idxStart > 15 {
		return fmt.Errorf("calibration offset index start must be [5-15]")
	}
	if idxNb > 8 {
		return fmt.Errorf("calibration offset index number must be <= 8")
	}
	if err := h.SoftReset(); err != nil {
		return err
	}
	if err := h.RegW(LGW_GLOBAL_EN, 0); err != nil {
		return err
	}
	if err := h.RegW(LGW_CLK32M_EN, 0); err != nil {
		return err
	}
	if err := h.RegW(LGW_RADIO_RST, 1); err != nil {
		return err
	}
	waitMs(5)
	if err := h.RegW(LGW_RADIO_RST, 0); err != nil {
		return err
	}
	for i := 0; i < RFChainNb; i++ {
		if err := h.setupSX125x(uint8(i), h.rfClkout == uint8(i), h.rfEnable[i], h.rfRadioType[i], h.rfRxFreq[i]); err != nil {
			return fmt.Errorf("setup radio %d: %w", i, err)
		}
	}
	if err := h.RegW(LGW_GLOBAL_EN, 1); err != nil {
		return err
	}
	if err := h.RegW(LGW_CLK32M_EN, 1); err != nil {
		return err
	}

	calCmd := uint8(0x10) // DAC gain = 3
	if h.rfEnable[0] {
		calCmd |= 0x01
	}
	if h.rfEnable[1] {
		calCmd |= 0x02
	}
	if h.rfEnable[0] && h.rfTxEnable[0] {
		calCmd |= 0x04
	}
	if h.rfEnable[1] && h.rfTxEnable[1] {
		calCmd |= 0x08
	}
	if h.rfRadioType[0] == RadioTypeSX1255 {
		calCmd |= 0x20
	}

	if err := h.loadFirmware(mcuAGC, calFW); err != nil {
		return err
	}
	if err := h.RegW(LGW_FORCE_HOST_RADIO_CTRL, 0); err != nil {
		return err
	}
	if err := h.RegW(LGW_RADIO_SELECT, int32(calCmd)); err != nil {
		return err
	}
	if err := h.RegW(LGW_MCU_RST_1, 0); err != nil {
		return err
	}

	if err := h.RegW(LGW_DBG_AGC_MCU_RAM_ADDR, fwVersionAddr); err != nil {
		return err
	}
	v, err := h.RegR(LGW_DBG_AGC_MCU_RAM_DATA)
	if err != nil {
		return err
	}
	if v != fwVersionCal {
		return fmt.Errorf("unexpected calibration firmware version %d", v)
	}

	if err := h.RegW(LGW_PAGE_REG, 3); err != nil {
		return err
	}
	if err := h.RegW(LGW_EMERGENCY_FORCE_HOST_CTRL, 0); err != nil {
		return err
	}
	waitMs(2300) // measured calibration time
	if err := h.RegW(LGW_EMERGENCY_FORCE_HOST_CTRL, 1); err != nil {
		return err
	}

	status, err := h.RegR(LGW_MCU_AGC_STATUS)
	if err != nil {
		return err
	}
	if status&0x81 != 0x81 {
		return fmt.Errorf("calibration failure (status = 0x%X)", status)
	}
	if h.rfEnable[0] && status&0x02 == 0 {
		return errors.New("calibration could not access radio A")
	}
	if h.rfEnable[1] && status&0x04 == 0 {
		return errors.New("calibration could not access radio B")
	}

	for i := 0; i < idxNb; i++ {
		read := func(addr int32) (int8, error) {
			if err := h.RegW(LGW_DBG_AGC_MCU_RAM_ADDR, addr); err != nil {
				return 0, err
			}
			v, err := h.RegR(LGW_DBG_AGC_MCU_RAM_DATA)
			return int8(v), err
		}
		if h.calOffsetAI[i], err = read(int32(0xA0 + i)); err != nil {
			return err
		}
		if h.calOffsetAQ[i], err = read(int32(0xA8 + i)); err != nil {
			return err
		}
		if h.calOffsetBI[i], err = read(int32(0xB0 + i)); err != nil {
			return err
		}
		if h.calOffsetBQ[i], err = read(int32(0xB8 + i)); err != nil {
			return err
		}
	}
	// require MCU to capture calibration values
	_, err = h.c.Command('j', 0, []byte{uint8(idxStart), uint8(idxNb)})
	return err
}

// --- constant adjustments (port of lgw_constant_adjust) --------------------

func (h *Hal) constantAdjust() error {
	w := func(id, v int) error { return h.RegW(id, int32(v)) }
	pub1, pub2 := int32(1), int32(2)
	if h.lorawanPublic {
		pub1, pub2 = 3, 4
	}
	steps := [][2]int{
		{LGW_RSSI_BB_FILTER_ALPHA, 6},
		{LGW_RSSI_DEC_FILTER_ALPHA, 7},
		{LGW_RSSI_CHANN_FILTER_ALPHA, 7},
		{LGW_RSSI_BB_DEFAULT_VALUE, 23},
		{LGW_RSSI_CHANN_DEFAULT_VALUE, 85},
		{LGW_RSSI_DEC_DEFAULT_VALUE, 66},
		{LGW_DEC_GAIN_OFFSET, 7},
		{LGW_CHAN_GAIN_OFFSET, 6},
		{LGW_SNR_AVG_CST, 3},
		{LGW_FRAME_SYNCH_PEAK1_POS, int(pub1)},
		{LGW_FRAME_SYNCH_PEAK2_POS, int(pub2)},
		{LGW_MBWSSF_FRAME_SYNCH_PEAK1_POS, int(pub1)},
		{LGW_MBWSSF_FRAME_SYNCH_PEAK2_POS, int(pub2)},
		{LGW_ADJUST_MODEM_START_OFFSET_RDX4, 1},
		{LGW_ADJUST_MODEM_START_OFFSET_SF12_RDX4, 4094},
		{LGW_CORR_MAC_GAIN, 7},
		{LGW_FSK_RX_INVERT, 1},
		{LGW_FSK_MODEM_INVERT_IQ, 1},
		{LGW_FSK_RSSI_LENGTH, 4},
		{LGW_FSK_PKT_MODE, 1},
		{LGW_FSK_CRC_EN, 1},
		{LGW_FSK_DCFREE_ENC, 2},
		{LGW_FSK_ERROR_OSR_TOL, 10},
		{LGW_FSK_PKT_LENGTH, 255},
		{LGW_FSK_PATTERN_TIMEOUT_CFG, 128},
		{LGW_TX_START_DELAY, txStartDelay},
		{LGW_TX_SWAP_IQ, 1},
		{LGW_TX_FRAME_SYNCH_PEAK1_POS, int(pub1)},
		{LGW_TX_FRAME_SYNCH_PEAK2_POS, int(pub2)},
		{LGW_FSK_TX_GAUSSIAN_SELECT_BT, 2},
	}
	for _, s := range steps {
		if err := w(s[0], s[1]); err != nil {
			return err
		}
	}
	return nil
}

// --- configuration (ports of lgw_*_setconf, talking to the MCU) ------------

func (h *Hal) checkStopped(op string) error {
	if h.started {
		return fmt.Errorf("concentrator is running, stop it before %s", op)
	}
	return nil
}

// BoardSetconf applies board-level configuration.
func (h *Hal) BoardSetconf(conf BoardConf) error {
	if err := h.checkStopped("board configuration"); err != nil {
		return err
	}
	h.lorawanPublic = conf.LorawanPublic
	h.rfClkout = conf.ClkSrc
	ans, err := h.c.Command('i', 0, []byte{uint8(b2i(conf.LorawanPublic)), conf.ClkSrc, 0, 0})
	if err != nil {
		return err
	}
	if ans.Status != 1 {
		return errors.New("board configuration: MCU ACK failed")
	}
	return nil
}

// RxrfSetconf configures an RF chain.
func (h *Hal) RxrfSetconf(rfChain uint8, conf RxRFConf) error {
	if err := h.checkStopped("RF chain configuration"); err != nil {
		return err
	}
	if rfChain >= RFChainNb {
		return fmt.Errorf("invalid rf_chain %d", rfChain)
	}
	if conf.Type != RadioTypeSX1255 && conf.Type != RadioTypeSX1257 {
		return fmt.Errorf("invalid radio type %d", conf.Type)
	}
	h.rfEnable[rfChain] = conf.Enable
	h.rfRxFreq[rfChain] = conf.FreqHz
	h.rfRssiOffset[rfChain] = conf.RssiOffset
	h.rfRadioType[rfChain] = conf.Type
	h.rfTxEnable[rfChain] = conf.TxEnable

	data := make([]byte, 20)
	if conf.Enable {
		data[0] = 1
	}
	binary.LittleEndian.PutUint32(data[4:], conf.FreqHz)
	binary.LittleEndian.PutUint32(data[8:], math.Float32bits(conf.RssiOffset))
	data[12] = uint8(conf.Type)
	binary.LittleEndian.PutUint32(data[16:], uint32(b2i(conf.TxEnable)))
	ans, err := h.c.Command('c', rfChain, data)
	if err != nil {
		return err
	}
	if ans.Status != 1 {
		return fmt.Errorf("rxrf %d configuration: MCU ACK failed", rfChain)
	}
	return nil
}

func bwVal(bw uint8) int32 {
	switch bw {
	case BW500kHz:
		return 500000
	case BW250kHz:
		return 250000
	case BW125kHz:
		return 125000
	case BW62k5Hz:
		return 62500
	case BW31k2Hz:
		return 31200
	case BW15k6Hz:
		return 15600
	case BW7k8Hz:
		return 7800
	default:
		return -1
	}
}

func isLoraBW(bw uint8) bool { return bw == BW125kHz || bw == BW250kHz || bw == BW500kHz }
func isLoraStdDR(dr uint32) bool {
	return dr == DRLoraSF7 || dr == DRLoraSF8 || dr == DRLoraSF9 || dr == DRLoraSF10 || dr == DRLoraSF11 || dr == DRLoraSF12
}
func isLoraCR(cr uint8) bool { return cr >= CRLora45 && cr <= CRLora48 }

// RxifSetconf configures an IF chain + modem.
func (h *Hal) RxifSetconf(ifChain uint8, conf RxIFConf) error {
	if err := h.checkStopped("IF chain configuration"); err != nil {
		return err
	}
	if ifChain >= IFChainNb {
		return fmt.Errorf("invalid if_chain %d", ifChain)
	}
	if !conf.Enable {
		h.ifEnable[ifChain] = false
		h.ifFreq[ifChain] = 0
	} else {
		if conf.RFChain >= RFChainNb {
			return fmt.Errorf("invalid rf_chain %d for if_chain %d", conf.RFChain, ifChain)
		}
		var rfRxBandwidth int32
		switch conf.Bandwidth {
		case BW250kHz:
			rfRxBandwidth = 1000000
		case BW500kHz:
			rfRxBandwidth = 1100000
		default:
			rfRxBandwidth = 925000
		}
		bwHz := bwVal(conf.Bandwidth)
		if bwHz == -1 {
			bwHz = RefBW
		}
		if conf.FreqHz+bwHz/2 > rfRxBandwidth/2 || conf.FreqHz-bwHz/2 < -rfRxBandwidth/2 {
			return fmt.Errorf("if frequency %d out of range", conf.FreqHz)
		}
		switch ifModemConfig[ifChain] {
		case IFLoraStd:
			if conf.Bandwidth == BWUndefined {
				conf.Bandwidth = BW250kHz
			}
			if conf.Datarate == DRUndefined {
				conf.Datarate = DRLoraSF9
			}
			if !isLoraBW(conf.Bandwidth) || !isLoraStdDR(conf.Datarate) {
				return fmt.Errorf("bad bw/dr for std chain %d", ifChain)
			}
			h.ifEnable[ifChain] = true
			h.ifRFChain[ifChain] = conf.RFChain
			h.ifFreq[ifChain] = conf.FreqHz
			h.loraRxBW = conf.Bandwidth
			h.loraRxSF = uint8(conf.Datarate & 0xFF)
			h.loraRxPPMOffset = (conf.Bandwidth == BW125kHz && (conf.Datarate == DRLoraSF11 || conf.Datarate == DRLoraSF12)) ||
				(conf.Bandwidth == BW250kHz && conf.Datarate == DRLoraSF12)
		case IFLoraMulti:
			if conf.Bandwidth == BWUndefined {
				conf.Bandwidth = BW125kHz
			}
			if conf.Datarate == DRUndefined {
				conf.Datarate = DRLoraMulti
			}
			if conf.Bandwidth != BW125kHz {
				return fmt.Errorf("multi chain only supports 125kHz")
			}
			if conf.Datarate&^DRLoraMulti != 0 {
				return fmt.Errorf("invalid multi SF mask %#x", conf.Datarate)
			}
			h.ifEnable[ifChain] = true
			h.ifRFChain[ifChain] = conf.RFChain
			h.ifFreq[ifChain] = conf.FreqHz
			h.sfmask[ifChain] = uint8(conf.Datarate & DRLoraMulti)
		case IFFskStd:
			if conf.Bandwidth == BWUndefined {
				conf.Bandwidth = BW250kHz
			}
			if conf.Datarate == DRUndefined {
				conf.Datarate = 64000
			}
			if conf.Bandwidth < 1 || conf.Bandwidth > 7 || conf.Datarate < DRFSKMin || conf.Datarate > DRFSKMax {
				return fmt.Errorf("bad bw/dr for FSK chain %d", ifChain)
			}
			h.ifEnable[ifChain] = true
			h.ifRFChain[ifChain] = conf.RFChain
			h.ifFreq[ifChain] = conf.FreqHz
			h.fskRxBW = conf.Bandwidth
			h.fskRxDR = conf.Datarate
			if conf.SyncWord > 0 {
				h.fskSyncWordSize = conf.SyncWordSize
				h.fskSyncWord = conf.SyncWord
			}
		default:
			return fmt.Errorf("if chain %d type not supported", ifChain)
		}
	}

	data := make([]byte, 32)
	if conf.Enable {
		data[0] = 1
	}
	data[1] = conf.RFChain
	binary.LittleEndian.PutUint32(data[4:], uint32(conf.FreqHz))
	data[8] = conf.Bandwidth
	binary.LittleEndian.PutUint32(data[12:], conf.Datarate)
	data[16] = conf.SyncWordSize
	binary.LittleEndian.PutUint64(data[24:], conf.SyncWord)
	ans, err := h.c.Command('d', ifChain, data)
	if err != nil {
		return err
	}
	if ans.Status != 1 {
		return fmt.Errorf("rxif %d configuration: MCU ACK failed", ifChain)
	}
	return nil
}

// TxgainSetconf sets the TX gain LUT.
func (h *Hal) TxgainSetconf(lut TxGainLUT) error {
	if lut.Size < 1 || lut.Size > TxGainLUTSizeMax {
		return fmt.Errorf("LUT must have 1..%d entries", TxGainLUTSizeMax)
	}
	for i := 0; i < lut.Size; i++ {
		g := lut.LUT[i]
		if g.DigGain > 3 {
			return errors.New("dig_gain must be 0..3")
		}
		if g.DacGain != 3 {
			return errors.New("dac_gain != 3 not supported")
		}
		if g.MixGain > 15 {
			return errors.New("mix_gain must not exceed 15")
		}
		if g.PaGain > 3 {
			return errors.New("pa_gain must not exceed 3")
		}
	}
	h.txGainLUT = lut
	data := make([]byte, TxGainLUTSizeMax*5+1)
	for i := 0; i < lut.Size; i++ {
		g := lut.LUT[i]
		data[i*5+0] = g.DigGain
		data[i*5+1] = g.PaGain
		data[i*5+2] = g.DacGain
		data[i*5+3] = g.MixGain
		data[i*5+4] = uint8(g.RFPower)
	}
	data[TxGainLUTSizeMax*5] = uint8(lut.Size)
	ans, err := h.c.Command('h', 0, data)
	if err != nil {
		return err
	}
	if ans.Status != 1 {
		return errors.New("txgain configuration: MCU ACK failed")
	}
	return nil
}

// --- start (port of lgw_start) ----------------------------------------------

func ifHzToReg(f int32) int32 { return (f << 5) / 15625 }

// Start calibrates the radios, uploads the SX1301 MCU firmwares and starts
// reception according to the previously applied configuration.
func (h *Hal) Start() error {
	if err := h.calibrateSX125x(calLowFW, 5, 8); err != nil {
		return fmt.Errorf("calibration (5-12): %w", err)
	}
	waitMs(5)
	if err := h.calibrateSX125x(calFW, 8, 8); err != nil {
		return fmt.Errorf("calibration (8-15): %w", err)
	}
	if err := h.RegW(LGW_GPIO_MODE, 31); err != nil {
		return err
	}
	if err := h.RegW(LGW_GPIO_SELECT_OUTPUT, 0); err != nil {
		return err
	}
	if err := h.constantAdjust(); err != nil {
		return err
	}
	if h.rfRxFreq[0] == 0 {
		return errors.New("wrong configuration, rf_rx_freq[0] is not set")
	}
	x := 4096000000 / (h.rfRxFreq[0] >> 1)
	if x > 63 {
		x = 63
	}
	if err := h.RegW(LGW_FREQ_TO_TIME_DRIFT, int32(x)); err != nil {
		return err
	}
	x = 4096000000 / (h.rfRxFreq[0] >> 3)
	if x > 63 {
		x = 63
	}
	if err := h.RegW(LGW_MBWSSF_FREQ_TO_TIME_DRIFT, int32(x)); err != nil {
		return err
	}

	var radioSelect uint8
	for i := 0; i < MultiNb; i++ {
		if h.ifRFChain[i] == 1 {
			radioSelect |= 1 << i
		}
	}
	for i := 0; i < 8; i++ {
		if err := h.RegW(LGW_IF_FREQ_0+i, ifHzToReg(h.ifFreq[i])); err != nil {
			return err
		}
		v := int32(0)
		if h.ifEnable[i] {
			v = int32(h.sfmask[i])
		}
		if err := h.RegW(LGW_CORR0_DETECT_EN+i, v); err != nil {
			return err
		}
	}
	if err := h.RegW(LGW_PPM_OFFSET, 0x60); err != nil {
		return err
	}
	if err := h.RegW(LGW_CONCENTRATOR_MODEM_ENABLE, 1); err != nil {
		return err
	}

	// LoRa stand-alone modem (IF8)
	if err := h.RegW(LGW_IF_FREQ_8, ifHzToReg(h.ifFreq[8])); err != nil {
		return err
	}
	if h.ifEnable[8] {
		if err := h.RegW(LGW_MBWSSF_RADIO_SELECT, int32(h.ifRFChain[8])); err != nil {
			return err
		}
		var bw int32
		switch h.loraRxBW {
		case BW125kHz:
			bw = 0
		case BW250kHz:
			bw = 1
		case BW500kHz:
			bw = 2
		default:
			return fmt.Errorf("unexpected std modem bw %#x", h.loraRxBW)
		}
		if err := h.RegW(LGW_MBWSSF_MODEM_BW, bw); err != nil {
			return err
		}
		var sf int32
		switch h.loraRxSF {
		case DRLoraSF7:
			sf = 7
		case DRLoraSF8:
			sf = 8
		case DRLoraSF9:
			sf = 9
		case DRLoraSF10:
			sf = 10
		case DRLoraSF11:
			sf = 11
		case DRLoraSF12:
			sf = 12
		default:
			return fmt.Errorf("unexpected std modem sf %#x", h.loraRxSF)
		}
		if err := h.RegW(LGW_MBWSSF_RATE_SF, sf); err != nil {
			return err
		}
		if err := h.RegW(LGW_MBWSSF_PPM_OFFSET, b2i(h.loraRxPPMOffset)); err != nil {
			return err
		}
		if err := h.RegW(LGW_MBWSSF_MODEM_ENABLE, 1); err != nil {
			return err
		}
	} else {
		if err := h.RegW(LGW_MBWSSF_MODEM_ENABLE, 0); err != nil {
			return err
		}
	}

	// FSK modem (IF9)
	if err := h.RegW(LGW_IF_FREQ_9, ifHzToReg(h.ifFreq[9])); err != nil {
		return err
	}
	if err := h.RegW(LGW_FSK_PSIZE, int32(h.fskSyncWordSize)-1); err != nil {
		return err
	}
	if err := h.RegW(LGW_FSK_TX_PSIZE, int32(h.fskSyncWordSize)-1); err != nil {
		return err
	}
	syncReg := h.fskSyncWord << (8 * (8 - h.fskSyncWordSize))
	if err := h.RegW(LGW_FSK_REF_PATTERN_LSB, int32(uint32(syncReg))); err != nil {
		return err
	}
	if err := h.RegW(LGW_FSK_REF_PATTERN_MSB, int32(uint32(syncReg>>32))); err != nil {
		return err
	}
	if h.ifEnable[9] {
		if err := h.RegW(LGW_FSK_RADIO_SELECT, int32(h.ifRFChain[9])); err != nil {
			return err
		}
		if err := h.RegW(LGW_FSK_BR_RATIO, int32(XtalFreq/h.fskRxDR)); err != nil {
			return err
		}
		if err := h.RegW(LGW_FSK_CH_BW_EXPO, int32(h.fskRxBW)); err != nil {
			return err
		}
		if err := h.RegW(LGW_FSK_MODEM_ENABLE, 1); err != nil {
			return err
		}
	} else {
		if err := h.RegW(LGW_FSK_MODEM_ENABLE, 0); err != nil {
			return err
		}
	}

	if err := h.loadFirmware(mcuARB, arbFW); err != nil {
		return fmt.Errorf("loading arbiter firmware: %w", err)
	}
	if err := h.loadFirmware(mcuAGC, agcFW); err != nil {
		return fmt.Errorf("loading AGC firmware: %w", err)
	}
	for _, s := range [][2]int{
		{LGW_FORCE_HOST_RADIO_CTRL, 0},
		{LGW_FORCE_HOST_FE_CTRL, 0},
		{LGW_FORCE_DEC_FILTER_GAIN, 0},
		{LGW_RADIO_SELECT, 0},
		{LGW_MCU_RST_0, 0},
		{LGW_MCU_RST_1, 0},
	} {
		if err := h.RegW(s[0], int32(s[1])); err != nil {
			return err
		}
	}
	checkFW := func(addrReg, dataReg int, want int32, name string) error {
		if err := h.RegW(addrReg, fwVersionAddr); err != nil {
			return err
		}
		v, err := h.RegR(dataReg)
		if err != nil {
			return err
		}
		if v != want {
			return fmt.Errorf("%s firmware version %d, expected %d", name, v, want)
		}
		return nil
	}
	if err := checkFW(LGW_DBG_AGC_MCU_RAM_ADDR, LGW_DBG_AGC_MCU_RAM_DATA, fwVersionAGC, "AGC"); err != nil {
		return err
	}
	if err := checkFW(LGW_DBG_ARB_MCU_RAM_ADDR, LGW_DBG_ARB_MCU_RAM_DATA, fwVersionARB, "arbiter"); err != nil {
		return err
	}
	waitMs(10)

	// AGC initialization handshake
	expect := func(want int32) error {
		v, err := h.RegR(LGW_MCU_AGC_STATUS)
		if err != nil {
			return err
		}
		if v != want {
			return fmt.Errorf("AGC firmware init failure, status 0x%02X (expected 0x%02X)", v, want)
		}
		return nil
	}
	if err := expect(0x10); err != nil {
		return err
	}
	for i := 0; i < h.txGainLUT.Size; i++ {
		g := h.txGainLUT.LUT[i]
		if err := h.RegW(LGW_RADIO_SELECT, agcCmdWait); err != nil {
			return err
		}
		waitMs(1)
		loadVal := int32(g.MixGain) + 16*int32(g.DacGain) + 64*int32(g.PaGain)
		if err := h.RegW(LGW_RADIO_SELECT, loadVal); err != nil {
			return err
		}
		waitMs(1)
		if err := expect(int32(0x30 + i)); err != nil {
			return err
		}
	}
	if h.txGainLUT.Size < TxGainLUTSizeMax {
		if err := h.RegW(LGW_RADIO_SELECT, agcCmdWait); err != nil {
			return err
		}
		waitMs(10)
		if err := h.RegW(LGW_RADIO_SELECT, agcCmdAbort); err != nil {
			return err
		}
		waitMs(10)
		if err := expect(0x30); err != nil {
			return err
		}
	}
	// TX freq MSBs (always 3 for SX1257 > 768MHz) then chan_select then final radio_select
	for _, cmd := range []int32{3, 0} {
		if err := h.RegW(LGW_RADIO_SELECT, agcCmdWait); err != nil {
			return err
		}
		waitMs(10)
		if err := h.RegW(LGW_RADIO_SELECT, cmd); err != nil {
			return err
		}
		waitMs(10)
	}
	if err := expect(0x30); err != nil {
		return err
	}
	if err := h.RegW(LGW_RADIO_SELECT, agcCmdWait); err != nil {
		return err
	}
	waitMs(10)
	if err := h.RegW(LGW_RADIO_SELECT, int32(radioSelect)); err != nil {
		return err
	}
	waitMs(10)
	if err := expect(0x40); err != nil {
		return err
	}
	if err := h.RegW(LGW_GPS_EN, 0); err != nil {
		return err
	}
	h.started = true
	return nil
}

// Stop resets the chip; it does not close the bridge.
func (h *Hal) Stop() error {
	h.started = false
	return h.SoftReset()
}

// --- runtime operations ------------------------------------------------------

// Receive fetches up to maxPkt packets from the concentrator FIFO.
func (h *Hal) Receive(maxPkt uint8) ([]RxPacket, error) {
	if maxPkt == 0 || maxPkt > PktFIFOSize {
		return nil, fmt.Errorf("invalid max_pkt %d", maxPkt)
	}
	ans, err := h.c.Command('b', 0, []byte{maxPkt})
	if err != nil {
		return nil, err
	}
	if ans.Status != 1 || len(ans.Data) < 1 {
		return nil, nil
	}
	nb := int(ans.Data[0])
	if nb > PktFIFOSize {
		return nil, fmt.Errorf("invalid packet count %d", nb)
	}
	pkts := make([]RxPacket, 0, nb)
	cur := 1
	for i := 0; i < nb; i++ {
		if cur+44 > len(ans.Data) {
			break
		}
		meta := ans.Data[cur : cur+44]
		size := binary.LittleEndian.Uint16(meta[42:44])
		if int(size) > 255 {
			size = 255
		}
		if cur+44+int(size) > len(ans.Data) {
			break
		}
		var p RxPacket
		p.FreqHz = binary.LittleEndian.Uint32(meta[0:4])
		p.IFChain = meta[4]
		p.Status = meta[5]
		p.CountUs = binary.LittleEndian.Uint32(meta[8:12])
		p.RFChain = meta[12]
		p.Modulation = meta[13]
		p.Bandwidth = meta[14]
		p.Datarate = binary.LittleEndian.Uint32(meta[16:20])
		p.Coderate = meta[20]
		p.Rssi = math.Float32frombits(binary.LittleEndian.Uint32(meta[24:28]))
		p.Snr = math.Float32frombits(binary.LittleEndian.Uint32(meta[28:32]))
		p.SnrMin = math.Float32frombits(binary.LittleEndian.Uint32(meta[32:36]))
		p.SnrMax = math.Float32frombits(binary.LittleEndian.Uint32(meta[36:40]))
		p.Crc = binary.LittleEndian.Uint16(meta[40:42])
		p.Size = size
		p.Payload = append([]byte(nil), ans.Data[cur+44:cur+44+int(size)]...)
		pkts = append(pkts, p)
		cur += 44 + int(size)
	}
	return pkts, nil
}

// Send schedules a transmission.
func (h *Hal) Send(pkt TxPacket) error {
	if pkt.RFChain >= RFChainNb {
		return fmt.Errorf("invalid rf_chain %d", pkt.RFChain)
	}
	if h.started && !h.rfTxEnable[pkt.RFChain] {
		return fmt.Errorf("rf_chain %d is disabled for TX", pkt.RFChain)
	}
	if h.started && !h.rfEnable[pkt.RFChain] {
		return fmt.Errorf("rf_chain %d is disabled", pkt.RFChain)
	}
	if pkt.TxMode > TxOnGPS {
		return fmt.Errorf("invalid tx_mode %d", pkt.TxMode)
	}
	if pkt.Modulation == ModLora {
		if !isLoraBW(pkt.Bandwidth) || !isLoraStdDR(pkt.Datarate) || !isLoraCR(pkt.Coderate) {
			return errors.New("invalid LoRa bw/dr/cr for TX")
		}
	} else if pkt.Modulation == ModFsk {
		if pkt.FDev < 1 || pkt.FDev > 200 || pkt.Datarate < DRFSKMin || pkt.Datarate > DRFSKMax {
			return errors.New("invalid FSK params for TX")
		}
	} else {
		return fmt.Errorf("invalid modulation %#x", pkt.Modulation)
	}
	if len(pkt.Payload) > 255 {
		return errors.New("payload too big")
	}
	pkt.Size = uint16(len(pkt.Payload))

	data := make([]byte, 30+256)
	binary.LittleEndian.PutUint32(data[0:], pkt.FreqHz)
	data[4] = pkt.TxMode
	binary.LittleEndian.PutUint32(data[8:], pkt.CountUs)
	data[12] = pkt.RFChain
	data[13] = uint8(pkt.RFPower)
	data[14] = pkt.Modulation
	data[15] = pkt.Bandwidth
	binary.LittleEndian.PutUint32(data[16:], pkt.Datarate)
	data[20] = pkt.Coderate
	data[21] = uint8(b2i(pkt.InvertPol))
	data[22] = pkt.FDev
	binary.LittleEndian.PutUint16(data[24:], pkt.Preamble)
	data[26] = uint8(b2i(pkt.NoCRC))
	data[27] = uint8(b2i(pkt.NoHeader))
	binary.LittleEndian.PutUint16(data[28:], pkt.Size)
	copy(data[30:], pkt.Payload)
	ans, err := h.c.Command('f', 0, data)
	if err != nil {
		return err
	}
	if ans.Status != 1 {
		return errors.New("send: MCU ACK failed")
	}
	return nil
}

// TxStatus returns the TX modem status (TxOff/TxFree/TxScheduled/TxEmitting).
func (h *Hal) TxStatus() (int, error) {
	if !h.started {
		return TxOff, nil
	}
	v, err := h.RegR(LGW_TX_STATUS)
	if err != nil {
		return TxStatusUnknown, err
	}
	switch {
	case v&0x10 == 0:
		return TxFree, nil
	case v&0x60 != 0:
		return TxEmitting, nil
	default:
		return TxScheduled, nil
	}
}

// AbortTx aborts a scheduled or ongoing TX.
func (h *Hal) AbortTx() error { return h.RegW(LGW_TX_TRIG_ALL, 0) }

// TrigCount returns the concentrator's internal microsecond counter.
func (h *Hal) TrigCount() (uint32, error) {
	ans, err := h.c.Command('q', 0, nil)
	if err != nil {
		return 0, err
	}
	if ans.Status != 1 || len(ans.Data) < 4 {
		return 0, errors.New("trigcnt: no data from MCU")
	}
	return binary.BigEndian.Uint32(ans.Data[:4]), nil
}

// Started reports whether the concentrator was started.
func (h *Hal) Started() bool { return h.started }

// TimeOnAir returns the packet air time in milliseconds.
func TimeOnAir(pkt TxPacket, fskSyncWordSize uint8) uint32 {
	if pkt.Modulation == ModLora {
		bw := bwVal(pkt.Bandwidth)
		if bw < 0 {
			return 0
		}
		bwKHz := float64(bw) / 1e3
		var sf float64
		switch pkt.Datarate {
		case DRLoraSF7:
			sf = 7
		case DRLoraSF8:
			sf = 8
		case DRLoraSF9:
			sf = 9
		case DRLoraSF10:
			sf = 10
		case DRLoraSF11:
			sf = 11
		case DRLoraSF12:
			sf = 12
		default:
			return 0
		}
		tsym := math.Pow(2, sf) / bwKHz
		tpre := (8 + 4.25) * tsym
		hh := 0.0
		if pkt.NoHeader {
			hh = 1
		}
		de := 0.0
		if sf >= 11 {
			de = 1
		}
		nb := 8 + math.Ceil((8*float64(pkt.Size)-4*sf+28+16-20*hh)/(4*(sf-2*de)))*float64(pkt.Coderate+4)
		return uint32(tpre + nb*tsym)
	}
	if pkt.Modulation == ModFsk {
		crc := 2.0
		if pkt.NoCRC {
			crc = 0
		}
		return uint32(8*float64(pkt.Preamble+uint16(fskSyncWordSize)+1+pkt.Size+uint16(crc))/float64(pkt.Datarate)*1e3) + 1
	}
	return 0
}
