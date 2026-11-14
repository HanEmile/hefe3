package hal

import _ "embed"

// SX1301 MCU firmware blobs, extracted verbatim from Lora-net/picoGW_hal.

//go:embed fw/agc_fw.bin
var agcFW []byte

//go:embed fw/arb_fw.bin
var arbFW []byte

//go:embed fw/cal_fw.bin
var calFW []byte

//go:embed fw/cal_fw5_12.bin
var calLowFW []byte
