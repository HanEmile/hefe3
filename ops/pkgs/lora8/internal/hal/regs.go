// Code generated from Lora-net/picoGW_hal loragw_reg.c. DO NOT EDIT.
package hal

type sxReg struct {
	page int8
	addr uint8
	offs uint8
	sign bool
	leng uint8
	rdon bool
	dflt int32
}

var loregs = [326]sxReg{
	sxReg{page: -1, addr: 0, offs: 0, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_PAGE_REG
	sxReg{page: -1, addr: 0, offs: 7, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SOFT_RESET
	sxReg{page: -1, addr: 1, offs: 0, sign: false, leng: 8, rdon: true, dflt: 103},    // LGW_VERSION
	sxReg{page: -1, addr: 2, offs: 0, sign: false, leng: 16, rdon: false, dflt: 0},    // LGW_RX_DATA_BUF_ADDR
	sxReg{page: -1, addr: 4, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_RX_DATA_BUF_DATA
	sxReg{page: -1, addr: 5, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_TX_DATA_BUF_ADDR
	sxReg{page: -1, addr: 6, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_TX_DATA_BUF_DATA
	sxReg{page: -1, addr: 7, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_CAPTURE_RAM_ADDR
	sxReg{page: -1, addr: 8, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_CAPTURE_RAM_DATA
	sxReg{page: -1, addr: 9, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_MCU_PROM_ADDR
	sxReg{page: -1, addr: 10, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},    // LGW_MCU_PROM_DATA
	sxReg{page: -1, addr: 11, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},    // LGW_RX_PACKET_DATA_FIFO_NUM_STORED
	sxReg{page: -1, addr: 12, offs: 0, sign: false, leng: 16, rdon: true, dflt: 0},    // LGW_RX_PACKET_DATA_FIFO_ADDR_POINTER
	sxReg{page: -1, addr: 14, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},     // LGW_RX_PACKET_DATA_FIFO_STATUS
	sxReg{page: -1, addr: 15, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},     // LGW_RX_PACKET_DATA_FIFO_PAYLOAD_SIZE
	sxReg{page: -1, addr: 16, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_MBWSSF_MODEM_ENABLE
	sxReg{page: -1, addr: 16, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_CONCENTRATOR_MODEM_ENABLE
	sxReg{page: -1, addr: 16, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_FSK_MODEM_ENABLE
	sxReg{page: -1, addr: 16, offs: 3, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_GLOBAL_EN
	sxReg{page: -1, addr: 17, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_CLK32M_EN
	sxReg{page: -1, addr: 17, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_CLKHS_EN
	sxReg{page: -1, addr: 18, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_START_BIST0
	sxReg{page: -1, addr: 18, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_START_BIST1
	sxReg{page: -1, addr: 18, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_CLEAR_BIST0
	sxReg{page: -1, addr: 18, offs: 3, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_CLEAR_BIST1
	sxReg{page: -1, addr: 19, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_BIST0_FINISHED
	sxReg{page: -1, addr: 19, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_BIST1_FINISHED
	sxReg{page: -1, addr: 20, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_AGC_PROG_RAM_BIST_STATUS
	sxReg{page: -1, addr: 20, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_ARB_PROG_RAM_BIST_STATUS
	sxReg{page: -1, addr: 20, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CAPTURE_RAM_BIST_STATUS
	sxReg{page: -1, addr: 20, offs: 3, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CHAN_FIR_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 20, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CHAN_FIR_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR0_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR1_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR2_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 3, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR3_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR4_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 5, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR5_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 6, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR6_RAM_BIST_STATUS
	sxReg{page: -1, addr: 21, offs: 7, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_CORR7_RAM_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM0_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM1_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM2_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 3, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM3_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM4_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 5, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM5_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 6, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM6_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 22, offs: 7, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM7_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM0_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM1_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM2_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 3, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM3_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM4_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 5, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM5_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 6, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM6_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 23, offs: 7, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM7_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM0_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM1_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM2_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 3, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM3_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM4_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 5, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM5_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 6, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM6_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 24, offs: 7, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM7_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 25, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM_MBWSSF_RAM0_BIST_STATUS
	sxReg{page: -1, addr: 25, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM_MBWSSF_RAM1_BIST_STATUS
	sxReg{page: -1, addr: 25, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MODEM_MBWSSF_RAM2_BIST_STATUS
	sxReg{page: -1, addr: 26, offs: 0, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_AGC_DATA_RAM_BIST0_STATUS
	sxReg{page: -1, addr: 26, offs: 1, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_AGC_DATA_RAM_BIST1_STATUS
	sxReg{page: -1, addr: 26, offs: 2, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_ARB_DATA_RAM_BIST0_STATUS
	sxReg{page: -1, addr: 26, offs: 3, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_ARB_DATA_RAM_BIST1_STATUS
	sxReg{page: -1, addr: 26, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_TX_TOP_RAM_BIST0_STATUS
	sxReg{page: -1, addr: 26, offs: 5, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_TX_TOP_RAM_BIST1_STATUS
	sxReg{page: -1, addr: 26, offs: 6, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_DATA_MNGT_RAM_BIST0_STATUS
	sxReg{page: -1, addr: 26, offs: 7, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_DATA_MNGT_RAM_BIST1_STATUS
	sxReg{page: -1, addr: 27, offs: 0, sign: false, leng: 4, rdon: false, dflt: 0},    // LGW_GPIO_SELECT_INPUT
	sxReg{page: -1, addr: 28, offs: 0, sign: false, leng: 4, rdon: false, dflt: 0},    // LGW_GPIO_SELECT_OUTPUT
	sxReg{page: -1, addr: 29, offs: 0, sign: false, leng: 5, rdon: false, dflt: 0},    // LGW_GPIO_MODE
	sxReg{page: -1, addr: 30, offs: 0, sign: false, leng: 5, rdon: true, dflt: 0},     // LGW_GPIO_PIN_REG_IN
	sxReg{page: -1, addr: 31, offs: 0, sign: false, leng: 5, rdon: false, dflt: 0},    // LGW_GPIO_PIN_REG_OUT
	sxReg{page: -1, addr: 32, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},     // LGW_MCU_AGC_STATUS
	sxReg{page: -1, addr: 125, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},    // LGW_MCU_ARB_STATUS
	sxReg{page: -1, addr: 126, offs: 0, sign: false, leng: 8, rdon: true, dflt: 1},    // LGW_CHIP_ID
	sxReg{page: -1, addr: 127, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},   // LGW_EMERGENCY_FORCE_HOST_CTRL
	sxReg{page: 0, addr: 33, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_RX_INVERT_IQ
	sxReg{page: 0, addr: 33, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_MODEM_INVERT_IQ
	sxReg{page: 0, addr: 33, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_MBWSSF_MODEM_INVERT_IQ
	sxReg{page: 0, addr: 33, offs: 3, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_RX_EDGE_SELECT
	sxReg{page: 0, addr: 33, offs: 4, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_MISC_RADIO_EN
	sxReg{page: 0, addr: 33, offs: 5, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_MODEM_INVERT_IQ
	sxReg{page: 0, addr: 34, offs: 0, sign: false, leng: 4, rdon: false, dflt: 7},     // LGW_FILTER_GAIN
	sxReg{page: 0, addr: 35, offs: 0, sign: false, leng: 8, rdon: false, dflt: 240},   // LGW_RADIO_SELECT
	sxReg{page: 0, addr: 36, offs: 0, sign: true, leng: 13, rdon: false, dflt: -384},  // LGW_IF_FREQ_0
	sxReg{page: 0, addr: 38, offs: 0, sign: true, leng: 13, rdon: false, dflt: -128},  // LGW_IF_FREQ_1
	sxReg{page: 0, addr: 40, offs: 0, sign: true, leng: 13, rdon: false, dflt: 128},   // LGW_IF_FREQ_2
	sxReg{page: 0, addr: 42, offs: 0, sign: true, leng: 13, rdon: false, dflt: 384},   // LGW_IF_FREQ_3
	sxReg{page: 0, addr: 44, offs: 0, sign: true, leng: 13, rdon: false, dflt: -384},  // LGW_IF_FREQ_4
	sxReg{page: 0, addr: 46, offs: 0, sign: true, leng: 13, rdon: false, dflt: -128},  // LGW_IF_FREQ_5
	sxReg{page: 0, addr: 48, offs: 0, sign: true, leng: 13, rdon: false, dflt: 128},   // LGW_IF_FREQ_6
	sxReg{page: 0, addr: 50, offs: 0, sign: true, leng: 13, rdon: false, dflt: 384},   // LGW_IF_FREQ_7
	sxReg{page: 0, addr: 52, offs: 0, sign: true, leng: 13, rdon: false, dflt: 0},     // LGW_IF_FREQ_8
	sxReg{page: 0, addr: 54, offs: 0, sign: true, leng: 13, rdon: false, dflt: 0},     // LGW_IF_FREQ_9
	sxReg{page: 0, addr: 64, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_CHANN_OVERRIDE_AGC_GAIN
	sxReg{page: 0, addr: 64, offs: 1, sign: false, leng: 4, rdon: false, dflt: 7},     // LGW_CHANN_AGC_GAIN
	sxReg{page: 0, addr: 65, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR0_DETECT_EN
	sxReg{page: 0, addr: 66, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR1_DETECT_EN
	sxReg{page: 0, addr: 67, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR2_DETECT_EN
	sxReg{page: 0, addr: 68, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR3_DETECT_EN
	sxReg{page: 0, addr: 69, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR4_DETECT_EN
	sxReg{page: 0, addr: 70, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR5_DETECT_EN
	sxReg{page: 0, addr: 71, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR6_DETECT_EN
	sxReg{page: 0, addr: 72, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},     // LGW_CORR7_DETECT_EN
	sxReg{page: 0, addr: 73, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_CORR_SAME_PEAKS_OPTION_SF6
	sxReg{page: 0, addr: 73, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CORR_SAME_PEAKS_OPTION_SF7
	sxReg{page: 0, addr: 73, offs: 2, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CORR_SAME_PEAKS_OPTION_SF8
	sxReg{page: 0, addr: 73, offs: 3, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CORR_SAME_PEAKS_OPTION_SF9
	sxReg{page: 0, addr: 73, offs: 4, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CORR_SAME_PEAKS_OPTION_SF10
	sxReg{page: 0, addr: 73, offs: 5, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CORR_SAME_PEAKS_OPTION_SF11
	sxReg{page: 0, addr: 73, offs: 6, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CORR_SAME_PEAKS_OPTION_SF12
	sxReg{page: 0, addr: 74, offs: 0, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF6
	sxReg{page: 0, addr: 74, offs: 4, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF7
	sxReg{page: 0, addr: 75, offs: 0, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF8
	sxReg{page: 0, addr: 75, offs: 4, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF9
	sxReg{page: 0, addr: 76, offs: 0, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF10
	sxReg{page: 0, addr: 76, offs: 4, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF11
	sxReg{page: 0, addr: 77, offs: 0, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_SIG_NOISE_RATIO_SF12
	sxReg{page: 0, addr: 78, offs: 0, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_CORR_NUM_SAME_PEAK
	sxReg{page: 0, addr: 78, offs: 4, sign: false, leng: 3, rdon: false, dflt: 5},     // LGW_CORR_MAC_GAIN
	sxReg{page: 0, addr: 81, offs: 0, sign: false, leng: 12, rdon: false, dflt: 0},    // LGW_ADJUST_MODEM_START_OFFSET_RDX4
	sxReg{page: 0, addr: 83, offs: 0, sign: false, leng: 12, rdon: false, dflt: 4092}, // LGW_ADJUST_MODEM_START_OFFSET_SF12_RDX4
	sxReg{page: 0, addr: 85, offs: 0, sign: false, leng: 8, rdon: false, dflt: 7},     // LGW_DBG_CORR_SELECT_SF
	sxReg{page: 0, addr: 86, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_DBG_CORR_SELECT_CHANNEL
	sxReg{page: 0, addr: 87, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_DBG_DETECT_CPT
	sxReg{page: 0, addr: 88, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_DBG_SYMB_CPT
	sxReg{page: 0, addr: 89, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_CHIRP_INVERT_RX
	sxReg{page: 0, addr: 89, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_DC_NOTCH_EN
	sxReg{page: 0, addr: 90, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_IMPLICIT_CRC_EN
	sxReg{page: 0, addr: 90, offs: 1, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_IMPLICIT_CODING_RATE
	sxReg{page: 0, addr: 91, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_IMPLICIT_PAYLOAD_LENGHT
	sxReg{page: 0, addr: 92, offs: 0, sign: false, leng: 8, rdon: false, dflt: 29},    // LGW_FREQ_TO_TIME_INVERT
	sxReg{page: 0, addr: 93, offs: 0, sign: false, leng: 6, rdon: false, dflt: 9},     // LGW_FREQ_TO_TIME_DRIFT
	sxReg{page: 0, addr: 94, offs: 0, sign: false, leng: 2, rdon: false, dflt: 2},     // LGW_PAYLOAD_FINE_TIMING_GAIN
	sxReg{page: 0, addr: 94, offs: 2, sign: false, leng: 2, rdon: false, dflt: 1},     // LGW_PREAMBLE_FINE_TIMING_GAIN
	sxReg{page: 0, addr: 94, offs: 4, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_TRACKING_INTEGRAL
	sxReg{page: 0, addr: 95, offs: 0, sign: false, leng: 4, rdon: false, dflt: 1},     // LGW_FRAME_SYNCH_PEAK1_POS
	sxReg{page: 0, addr: 95, offs: 4, sign: false, leng: 4, rdon: false, dflt: 2},     // LGW_FRAME_SYNCH_PEAK2_POS
	sxReg{page: 0, addr: 96, offs: 0, sign: false, leng: 16, rdon: false, dflt: 10},   // LGW_PREAMBLE_SYMB1_NB
	sxReg{page: 0, addr: 98, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_FRAME_SYNCH_GAIN
	sxReg{page: 0, addr: 98, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_SYNCH_DETECT_TH
	sxReg{page: 0, addr: 99, offs: 0, sign: false, leng: 4, rdon: false, dflt: 8},     // LGW_LLR_SCALE
	sxReg{page: 0, addr: 99, offs: 4, sign: false, leng: 2, rdon: false, dflt: 2},     // LGW_SNR_AVG_CST
	sxReg{page: 0, addr: 100, offs: 0, sign: false, leng: 7, rdon: false, dflt: 0},    // LGW_PPM_OFFSET
	sxReg{page: 0, addr: 101, offs: 0, sign: false, leng: 8, rdon: false, dflt: 255},  // LGW_MAX_PAYLOAD_LEN
	sxReg{page: 0, addr: 102, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_ONLY_CRC_EN
	sxReg{page: 0, addr: 103, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},    // LGW_ZERO_PAD
	sxReg{page: 0, addr: 104, offs: 0, sign: false, leng: 4, rdon: false, dflt: 8},    // LGW_DEC_GAIN_OFFSET
	sxReg{page: 0, addr: 104, offs: 4, sign: false, leng: 4, rdon: false, dflt: 7},    // LGW_CHAN_GAIN_OFFSET
	sxReg{page: 0, addr: 105, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_FORCE_HOST_RADIO_CTRL
	sxReg{page: 0, addr: 105, offs: 2, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_FORCE_HOST_FE_CTRL
	sxReg{page: 0, addr: 105, offs: 3, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_FORCE_DEC_FILTER_GAIN
	sxReg{page: 0, addr: 106, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_MCU_RST_0
	sxReg{page: 0, addr: 106, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},    // LGW_MCU_RST_1
	sxReg{page: 0, addr: 106, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_MCU_SELECT_MUX_0
	sxReg{page: 0, addr: 106, offs: 3, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_MCU_SELECT_MUX_1
	sxReg{page: 0, addr: 106, offs: 4, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_CORRUPTION_DETECTED_0
	sxReg{page: 0, addr: 106, offs: 5, sign: false, leng: 1, rdon: true, dflt: 0},     // LGW_MCU_CORRUPTION_DETECTED_1
	sxReg{page: 0, addr: 106, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_MCU_SELECT_EDGE_0
	sxReg{page: 0, addr: 106, offs: 7, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_MCU_SELECT_EDGE_1
	sxReg{page: 0, addr: 107, offs: 0, sign: false, leng: 8, rdon: false, dflt: 1},    // LGW_CHANN_SELECT_RSSI
	sxReg{page: 0, addr: 108, offs: 0, sign: false, leng: 8, rdon: false, dflt: 32},   // LGW_RSSI_BB_DEFAULT_VALUE
	sxReg{page: 0, addr: 109, offs: 0, sign: false, leng: 8, rdon: false, dflt: 100},  // LGW_RSSI_DEC_DEFAULT_VALUE
	sxReg{page: 0, addr: 110, offs: 0, sign: false, leng: 8, rdon: false, dflt: 100},  // LGW_RSSI_CHANN_DEFAULT_VALUE
	sxReg{page: 0, addr: 111, offs: 0, sign: false, leng: 5, rdon: false, dflt: 7},    // LGW_RSSI_BB_FILTER_ALPHA
	sxReg{page: 0, addr: 112, offs: 0, sign: false, leng: 5, rdon: false, dflt: 5},    // LGW_RSSI_DEC_FILTER_ALPHA
	sxReg{page: 0, addr: 113, offs: 0, sign: false, leng: 5, rdon: false, dflt: 8},    // LGW_RSSI_CHANN_FILTER_ALPHA
	sxReg{page: 0, addr: 114, offs: 0, sign: false, leng: 6, rdon: false, dflt: 0},    // LGW_IQ_MISMATCH_A_AMP_COEFF
	sxReg{page: 0, addr: 115, offs: 0, sign: false, leng: 6, rdon: false, dflt: 0},    // LGW_IQ_MISMATCH_A_PHI_COEFF
	sxReg{page: 0, addr: 116, offs: 0, sign: false, leng: 6, rdon: false, dflt: 0},    // LGW_IQ_MISMATCH_B_AMP_COEFF
	sxReg{page: 0, addr: 116, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},    // LGW_IQ_MISMATCH_B_SEL_I
	sxReg{page: 0, addr: 117, offs: 0, sign: false, leng: 6, rdon: false, dflt: 0},    // LGW_IQ_MISMATCH_B_PHI_COEFF
	sxReg{page: 1, addr: 33, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_TX_TRIG_IMMEDIATE
	sxReg{page: 1, addr: 33, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_TX_TRIG_DELAYED
	sxReg{page: 1, addr: 33, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_TX_TRIG_GPS
	sxReg{page: 1, addr: 34, offs: 0, sign: false, leng: 16, rdon: false, dflt: 0},    // LGW_TX_START_DELAY
	sxReg{page: 1, addr: 36, offs: 0, sign: false, leng: 4, rdon: false, dflt: 1},     // LGW_TX_FRAME_SYNCH_PEAK1_POS
	sxReg{page: 1, addr: 36, offs: 4, sign: false, leng: 4, rdon: false, dflt: 2},     // LGW_TX_FRAME_SYNCH_PEAK2_POS
	sxReg{page: 1, addr: 37, offs: 0, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_TX_RAMP_DURATION
	sxReg{page: 1, addr: 39, offs: 0, sign: true, leng: 8, rdon: false, dflt: 0},      // LGW_TX_OFFSET_I
	sxReg{page: 1, addr: 40, offs: 0, sign: true, leng: 8, rdon: false, dflt: 0},      // LGW_TX_OFFSET_Q
	sxReg{page: 1, addr: 41, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_TX_MODE
	sxReg{page: 1, addr: 41, offs: 1, sign: false, leng: 4, rdon: false, dflt: 0},     // LGW_TX_ZERO_PAD
	sxReg{page: 1, addr: 41, offs: 5, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_TX_EDGE_SELECT
	sxReg{page: 1, addr: 41, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_TX_EDGE_SELECT_TOP
	sxReg{page: 1, addr: 42, offs: 0, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_TX_GAIN
	sxReg{page: 1, addr: 42, offs: 2, sign: false, leng: 3, rdon: false, dflt: 5},     // LGW_TX_CHIRP_LOW_PASS
	sxReg{page: 1, addr: 42, offs: 5, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_TX_FCC_WIDEBAND
	sxReg{page: 1, addr: 42, offs: 7, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_TX_SWAP_IQ
	sxReg{page: 1, addr: 43, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_MBWSSF_IMPLICIT_HEADER
	sxReg{page: 1, addr: 43, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_MBWSSF_IMPLICIT_CRC_EN
	sxReg{page: 1, addr: 43, offs: 2, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_MBWSSF_IMPLICIT_CODING_RATE
	sxReg{page: 1, addr: 44, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_MBWSSF_IMPLICIT_PAYLOAD_LENGHT
	sxReg{page: 1, addr: 45, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_MBWSSF_AGC_FREEZE_ON_DETECT
	sxReg{page: 1, addr: 46, offs: 0, sign: false, leng: 4, rdon: false, dflt: 1},     // LGW_MBWSSF_FRAME_SYNCH_PEAK1_POS
	sxReg{page: 1, addr: 46, offs: 4, sign: false, leng: 4, rdon: false, dflt: 2},     // LGW_MBWSSF_FRAME_SYNCH_PEAK2_POS
	sxReg{page: 1, addr: 47, offs: 0, sign: false, leng: 16, rdon: false, dflt: 10},   // LGW_MBWSSF_PREAMBLE_SYMB1_NB
	sxReg{page: 1, addr: 49, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_MBWSSF_FRAME_SYNCH_GAIN
	sxReg{page: 1, addr: 49, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_MBWSSF_SYNCH_DETECT_TH
	sxReg{page: 1, addr: 50, offs: 0, sign: false, leng: 8, rdon: false, dflt: 10},    // LGW_MBWSSF_DETECT_MIN_SINGLE_PEAK
	sxReg{page: 1, addr: 51, offs: 0, sign: false, leng: 3, rdon: false, dflt: 3},     // LGW_MBWSSF_DETECT_TRIG_SAME_PEAK_NB
	sxReg{page: 1, addr: 52, offs: 0, sign: false, leng: 8, rdon: false, dflt: 29},    // LGW_MBWSSF_FREQ_TO_TIME_INVERT
	sxReg{page: 1, addr: 53, offs: 0, sign: false, leng: 6, rdon: false, dflt: 36},    // LGW_MBWSSF_FREQ_TO_TIME_DRIFT
	sxReg{page: 1, addr: 54, offs: 0, sign: false, leng: 12, rdon: false, dflt: 0},    // LGW_MBWSSF_PPM_CORRECTION
	sxReg{page: 1, addr: 56, offs: 0, sign: false, leng: 2, rdon: false, dflt: 2},     // LGW_MBWSSF_PAYLOAD_FINE_TIMING_GAIN
	sxReg{page: 1, addr: 56, offs: 2, sign: false, leng: 2, rdon: false, dflt: 1},     // LGW_MBWSSF_PREAMBLE_FINE_TIMING_GAIN
	sxReg{page: 1, addr: 56, offs: 4, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_MBWSSF_TRACKING_INTEGRAL
	sxReg{page: 1, addr: 57, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_MBWSSF_ZERO_PAD
	sxReg{page: 1, addr: 58, offs: 0, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_MBWSSF_MODEM_BW
	sxReg{page: 1, addr: 58, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_MBWSSF_RADIO_SELECT
	sxReg{page: 1, addr: 58, offs: 3, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_MBWSSF_RX_CHIRP_INVERT
	sxReg{page: 1, addr: 59, offs: 0, sign: false, leng: 4, rdon: false, dflt: 8},     // LGW_MBWSSF_LLR_SCALE
	sxReg{page: 1, addr: 59, offs: 4, sign: false, leng: 2, rdon: false, dflt: 3},     // LGW_MBWSSF_SNR_AVG_CST
	sxReg{page: 1, addr: 59, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_MBWSSF_PPM_OFFSET
	sxReg{page: 1, addr: 60, offs: 0, sign: false, leng: 4, rdon: false, dflt: 7},     // LGW_MBWSSF_RATE_SF
	sxReg{page: 1, addr: 60, offs: 4, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_MBWSSF_ONLY_CRC_EN
	sxReg{page: 1, addr: 61, offs: 0, sign: false, leng: 8, rdon: false, dflt: 255},   // LGW_MBWSSF_MAX_PAYLOAD_LEN
	sxReg{page: 1, addr: 62, offs: 0, sign: false, leng: 8, rdon: true, dflt: 128},    // LGW_TX_STATUS
	sxReg{page: 1, addr: 63, offs: 0, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_FSK_CH_BW_EXPO
	sxReg{page: 1, addr: 63, offs: 3, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_FSK_RSSI_LENGTH
	sxReg{page: 1, addr: 63, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_RX_INVERT
	sxReg{page: 1, addr: 63, offs: 7, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_PKT_MODE
	sxReg{page: 1, addr: 64, offs: 0, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_FSK_PSIZE
	sxReg{page: 1, addr: 64, offs: 3, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_CRC_EN
	sxReg{page: 1, addr: 64, offs: 4, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_FSK_DCFREE_ENC
	sxReg{page: 1, addr: 64, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_CRC_IBM
	sxReg{page: 1, addr: 65, offs: 0, sign: false, leng: 5, rdon: false, dflt: 0},     // LGW_FSK_ERROR_OSR_TOL
	sxReg{page: 1, addr: 65, offs: 7, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_RADIO_SELECT
	sxReg{page: 1, addr: 66, offs: 0, sign: false, leng: 16, rdon: false, dflt: 0},    // LGW_FSK_BR_RATIO
	sxReg{page: 1, addr: 68, offs: 0, sign: false, leng: 32, rdon: false, dflt: 0},    // LGW_FSK_REF_PATTERN_LSB
	sxReg{page: 1, addr: 72, offs: 0, sign: false, leng: 32, rdon: false, dflt: 0},    // LGW_FSK_REF_PATTERN_MSB
	sxReg{page: 1, addr: 76, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_FSK_PKT_LENGTH
	sxReg{page: 1, addr: 77, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_FSK_TX_GAUSSIAN_EN
	sxReg{page: 1, addr: 77, offs: 1, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_FSK_TX_GAUSSIAN_SELECT_BT
	sxReg{page: 1, addr: 77, offs: 3, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_FSK_TX_PATTERN_EN
	sxReg{page: 1, addr: 77, offs: 4, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_FSK_TX_PREAMBLE_SEQ
	sxReg{page: 1, addr: 77, offs: 5, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_FSK_TX_PSIZE
	sxReg{page: 1, addr: 80, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_FSK_NODE_ADRS
	sxReg{page: 1, addr: 81, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_FSK_BROADCAST
	sxReg{page: 1, addr: 82, offs: 0, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_FSK_AUTO_AFC_ON
	sxReg{page: 1, addr: 83, offs: 0, sign: false, leng: 10, rdon: false, dflt: 0},    // LGW_FSK_PATTERN_TIMEOUT_CFG
	sxReg{page: 2, addr: 33, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_SPI_RADIO_A__DATA
	sxReg{page: 2, addr: 34, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_SPI_RADIO_A__DATA_READBACK
	sxReg{page: 2, addr: 35, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_SPI_RADIO_A__ADDR
	sxReg{page: 2, addr: 37, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SPI_RADIO_A__CS
	sxReg{page: 2, addr: 38, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_SPI_RADIO_B__DATA
	sxReg{page: 2, addr: 39, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_SPI_RADIO_B__DATA_READBACK
	sxReg{page: 2, addr: 40, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_SPI_RADIO_B__ADDR
	sxReg{page: 2, addr: 42, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SPI_RADIO_B__CS
	sxReg{page: 2, addr: 43, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_RADIO_A_EN
	sxReg{page: 2, addr: 43, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_RADIO_B_EN
	sxReg{page: 2, addr: 43, offs: 2, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_RADIO_RST
	sxReg{page: 2, addr: 43, offs: 3, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_LNA_A_EN
	sxReg{page: 2, addr: 43, offs: 4, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_PA_A_EN
	sxReg{page: 2, addr: 43, offs: 5, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_LNA_B_EN
	sxReg{page: 2, addr: 43, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_PA_B_EN
	sxReg{page: 2, addr: 44, offs: 0, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_PA_GAIN
	sxReg{page: 2, addr: 45, offs: 0, sign: false, leng: 4, rdon: false, dflt: 2},     // LGW_LNA_A_CTRL_LUT
	sxReg{page: 2, addr: 45, offs: 4, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_PA_A_CTRL_LUT
	sxReg{page: 2, addr: 46, offs: 0, sign: false, leng: 4, rdon: false, dflt: 2},     // LGW_LNA_B_CTRL_LUT
	sxReg{page: 2, addr: 46, offs: 4, sign: false, leng: 4, rdon: false, dflt: 4},     // LGW_PA_B_CTRL_LUT
	sxReg{page: 2, addr: 47, offs: 0, sign: false, leng: 5, rdon: false, dflt: 0},     // LGW_CAPTURE_SOURCE
	sxReg{page: 2, addr: 47, offs: 5, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_CAPTURE_START
	sxReg{page: 2, addr: 47, offs: 6, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_CAPTURE_FORCE_TRIGGER
	sxReg{page: 2, addr: 47, offs: 7, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_CAPTURE_WRAP
	sxReg{page: 2, addr: 48, offs: 0, sign: false, leng: 16, rdon: false, dflt: 0},    // LGW_CAPTURE_PERIOD
	sxReg{page: 2, addr: 51, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_MODEM_STATUS
	sxReg{page: 2, addr: 52, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_VALID_HEADER_COUNTER_0
	sxReg{page: 2, addr: 54, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_VALID_PACKET_COUNTER_0
	sxReg{page: 2, addr: 56, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_VALID_HEADER_COUNTER_MBWSSF
	sxReg{page: 2, addr: 57, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_VALID_HEADER_COUNTER_FSK
	sxReg{page: 2, addr: 58, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_VALID_PACKET_COUNTER_MBWSSF
	sxReg{page: 2, addr: 59, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_VALID_PACKET_COUNTER_FSK
	sxReg{page: 2, addr: 60, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_CHANN_RSSI
	sxReg{page: 2, addr: 61, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_BB_RSSI
	sxReg{page: 2, addr: 62, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_DEC_RSSI
	sxReg{page: 2, addr: 63, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_DBG_MCU_DATA
	sxReg{page: 2, addr: 64, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_DBG_ARB_MCU_RAM_DATA
	sxReg{page: 2, addr: 65, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_DBG_AGC_MCU_RAM_DATA
	sxReg{page: 2, addr: 66, offs: 0, sign: false, leng: 16, rdon: true, dflt: 0},     // LGW_NEXT_PACKET_CNT
	sxReg{page: 2, addr: 68, offs: 0, sign: false, leng: 16, rdon: true, dflt: 0},     // LGW_ADDR_CAPTURE_COUNT
	sxReg{page: 2, addr: 70, offs: 0, sign: false, leng: 32, rdon: true, dflt: 0},     // LGW_TIMESTAMP
	sxReg{page: 2, addr: 74, offs: 0, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN0_GAIN
	sxReg{page: 2, addr: 74, offs: 4, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN1_GAIN
	sxReg{page: 2, addr: 75, offs: 0, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN2_GAIN
	sxReg{page: 2, addr: 75, offs: 4, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN3_GAIN
	sxReg{page: 2, addr: 76, offs: 0, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN4_GAIN
	sxReg{page: 2, addr: 76, offs: 4, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN5_GAIN
	sxReg{page: 2, addr: 77, offs: 0, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN6_GAIN
	sxReg{page: 2, addr: 77, offs: 4, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_CHANN7_GAIN
	sxReg{page: 2, addr: 78, offs: 0, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DBG_DEC_FILT_GAIN
	sxReg{page: 2, addr: 79, offs: 0, sign: false, leng: 3, rdon: true, dflt: 0},      // LGW_SPI_DATA_FIFO_PTR
	sxReg{page: 2, addr: 79, offs: 3, sign: false, leng: 3, rdon: true, dflt: 0},      // LGW_PACKET_DATA_FIFO_PTR
	sxReg{page: 2, addr: 80, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_DBG_ARB_MCU_RAM_ADDR
	sxReg{page: 2, addr: 81, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_DBG_AGC_MCU_RAM_ADDR
	sxReg{page: 2, addr: 82, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SPI_MASTER_CHIP_SELECT_POLARITY
	sxReg{page: 2, addr: 82, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SPI_MASTER_CPOL
	sxReg{page: 2, addr: 82, offs: 2, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SPI_MASTER_CPHA
	sxReg{page: 2, addr: 83, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SIG_GEN_ANALYSER_MUX_SEL
	sxReg{page: 2, addr: 84, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SIG_GEN_EN
	sxReg{page: 2, addr: 84, offs: 1, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_SIG_ANALYSER_EN
	sxReg{page: 2, addr: 84, offs: 2, sign: false, leng: 2, rdon: false, dflt: 0},     // LGW_SIG_ANALYSER_AVG_LEN
	sxReg{page: 2, addr: 84, offs: 4, sign: false, leng: 3, rdon: false, dflt: 0},     // LGW_SIG_ANALYSER_PRECISION
	sxReg{page: 2, addr: 84, offs: 7, sign: false, leng: 1, rdon: true, dflt: 0},      // LGW_SIG_ANALYSER_VALID_OUT
	sxReg{page: 2, addr: 85, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_SIG_GEN_FREQ
	sxReg{page: 2, addr: 86, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_SIG_ANALYSER_FREQ
	sxReg{page: 2, addr: 87, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_SIG_ANALYSER_I_OUT
	sxReg{page: 2, addr: 88, offs: 0, sign: false, leng: 8, rdon: true, dflt: 0},      // LGW_SIG_ANALYSER_Q_OUT
	sxReg{page: 2, addr: 89, offs: 0, sign: false, leng: 1, rdon: false, dflt: 0},     // LGW_GPS_EN
	sxReg{page: 2, addr: 89, offs: 1, sign: false, leng: 1, rdon: false, dflt: 1},     // LGW_GPS_POL
	sxReg{page: 2, addr: 90, offs: 0, sign: true, leng: 8, rdon: false, dflt: 0},      // LGW_SW_TEST_REG1
	sxReg{page: 2, addr: 91, offs: 2, sign: true, leng: 6, rdon: false, dflt: 0},      // LGW_SW_TEST_REG2
	sxReg{page: 2, addr: 92, offs: 0, sign: true, leng: 16, rdon: false, dflt: 0},     // LGW_SW_TEST_REG3
	sxReg{page: 2, addr: 94, offs: 0, sign: false, leng: 4, rdon: true, dflt: 0},      // LGW_DATA_MNGT_STATUS
	sxReg{page: 2, addr: 95, offs: 0, sign: false, leng: 5, rdon: true, dflt: 0},      // LGW_DATA_MNGT_CPT_FRAME_ALLOCATED
	sxReg{page: 2, addr: 96, offs: 0, sign: false, leng: 5, rdon: true, dflt: 0},      // LGW_DATA_MNGT_CPT_FRAME_FINISHED
	sxReg{page: 2, addr: 97, offs: 0, sign: false, leng: 5, rdon: true, dflt: 0},      // LGW_DATA_MNGT_CPT_FRAME_READEN
	sxReg{page: 1, addr: 33, offs: 0, sign: false, leng: 8, rdon: false, dflt: 0},     // LGW_TX_TRIG_ALL
}

const (
	LGW_PAGE_REG                            = 0
	LGW_SOFT_RESET                          = 1
	LGW_VERSION                             = 2
	LGW_RX_DATA_BUF_ADDR                    = 3
	LGW_RX_DATA_BUF_DATA                    = 4
	LGW_TX_DATA_BUF_ADDR                    = 5
	LGW_TX_DATA_BUF_DATA                    = 6
	LGW_CAPTURE_RAM_ADDR                    = 7
	LGW_CAPTURE_RAM_DATA                    = 8
	LGW_MCU_PROM_ADDR                       = 9
	LGW_MCU_PROM_DATA                       = 10
	LGW_RX_PACKET_DATA_FIFO_NUM_STORED      = 11
	LGW_RX_PACKET_DATA_FIFO_ADDR_POINTER    = 12
	LGW_RX_PACKET_DATA_FIFO_STATUS          = 13
	LGW_RX_PACKET_DATA_FIFO_PAYLOAD_SIZE    = 14
	LGW_MBWSSF_MODEM_ENABLE                 = 15
	LGW_CONCENTRATOR_MODEM_ENABLE           = 16
	LGW_FSK_MODEM_ENABLE                    = 17
	LGW_GLOBAL_EN                           = 18
	LGW_CLK32M_EN                           = 19
	LGW_CLKHS_EN                            = 20
	LGW_START_BIST0                         = 21
	LGW_START_BIST1                         = 22
	LGW_CLEAR_BIST0                         = 23
	LGW_CLEAR_BIST1                         = 24
	LGW_BIST0_FINISHED                      = 25
	LGW_BIST1_FINISHED                      = 26
	LGW_MCU_AGC_PROG_RAM_BIST_STATUS        = 27
	LGW_MCU_ARB_PROG_RAM_BIST_STATUS        = 28
	LGW_CAPTURE_RAM_BIST_STATUS             = 29
	LGW_CHAN_FIR_RAM0_BIST_STATUS           = 30
	LGW_CHAN_FIR_RAM1_BIST_STATUS           = 31
	LGW_CORR0_RAM_BIST_STATUS               = 32
	LGW_CORR1_RAM_BIST_STATUS               = 33
	LGW_CORR2_RAM_BIST_STATUS               = 34
	LGW_CORR3_RAM_BIST_STATUS               = 35
	LGW_CORR4_RAM_BIST_STATUS               = 36
	LGW_CORR5_RAM_BIST_STATUS               = 37
	LGW_CORR6_RAM_BIST_STATUS               = 38
	LGW_CORR7_RAM_BIST_STATUS               = 39
	LGW_MODEM0_RAM0_BIST_STATUS             = 40
	LGW_MODEM1_RAM0_BIST_STATUS             = 41
	LGW_MODEM2_RAM0_BIST_STATUS             = 42
	LGW_MODEM3_RAM0_BIST_STATUS             = 43
	LGW_MODEM4_RAM0_BIST_STATUS             = 44
	LGW_MODEM5_RAM0_BIST_STATUS             = 45
	LGW_MODEM6_RAM0_BIST_STATUS             = 46
	LGW_MODEM7_RAM0_BIST_STATUS             = 47
	LGW_MODEM0_RAM1_BIST_STATUS             = 48
	LGW_MODEM1_RAM1_BIST_STATUS             = 49
	LGW_MODEM2_RAM1_BIST_STATUS             = 50
	LGW_MODEM3_RAM1_BIST_STATUS             = 51
	LGW_MODEM4_RAM1_BIST_STATUS             = 52
	LGW_MODEM5_RAM1_BIST_STATUS             = 53
	LGW_MODEM6_RAM1_BIST_STATUS             = 54
	LGW_MODEM7_RAM1_BIST_STATUS             = 55
	LGW_MODEM0_RAM2_BIST_STATUS             = 56
	LGW_MODEM1_RAM2_BIST_STATUS             = 57
	LGW_MODEM2_RAM2_BIST_STATUS             = 58
	LGW_MODEM3_RAM2_BIST_STATUS             = 59
	LGW_MODEM4_RAM2_BIST_STATUS             = 60
	LGW_MODEM5_RAM2_BIST_STATUS             = 61
	LGW_MODEM6_RAM2_BIST_STATUS             = 62
	LGW_MODEM7_RAM2_BIST_STATUS             = 63
	LGW_MODEM_MBWSSF_RAM0_BIST_STATUS       = 64
	LGW_MODEM_MBWSSF_RAM1_BIST_STATUS       = 65
	LGW_MODEM_MBWSSF_RAM2_BIST_STATUS       = 66
	LGW_MCU_AGC_DATA_RAM_BIST0_STATUS       = 67
	LGW_MCU_AGC_DATA_RAM_BIST1_STATUS       = 68
	LGW_MCU_ARB_DATA_RAM_BIST0_STATUS       = 69
	LGW_MCU_ARB_DATA_RAM_BIST1_STATUS       = 70
	LGW_TX_TOP_RAM_BIST0_STATUS             = 71
	LGW_TX_TOP_RAM_BIST1_STATUS             = 72
	LGW_DATA_MNGT_RAM_BIST0_STATUS          = 73
	LGW_DATA_MNGT_RAM_BIST1_STATUS          = 74
	LGW_GPIO_SELECT_INPUT                   = 75
	LGW_GPIO_SELECT_OUTPUT                  = 76
	LGW_GPIO_MODE                           = 77
	LGW_GPIO_PIN_REG_IN                     = 78
	LGW_GPIO_PIN_REG_OUT                    = 79
	LGW_MCU_AGC_STATUS                      = 80
	LGW_MCU_ARB_STATUS                      = 81
	LGW_CHIP_ID                             = 82
	LGW_EMERGENCY_FORCE_HOST_CTRL           = 83
	LGW_RX_INVERT_IQ                        = 84
	LGW_MODEM_INVERT_IQ                     = 85
	LGW_MBWSSF_MODEM_INVERT_IQ              = 86
	LGW_RX_EDGE_SELECT                      = 87
	LGW_MISC_RADIO_EN                       = 88
	LGW_FSK_MODEM_INVERT_IQ                 = 89
	LGW_FILTER_GAIN                         = 90
	LGW_RADIO_SELECT                        = 91
	LGW_IF_FREQ_0                           = 92
	LGW_IF_FREQ_1                           = 93
	LGW_IF_FREQ_2                           = 94
	LGW_IF_FREQ_3                           = 95
	LGW_IF_FREQ_4                           = 96
	LGW_IF_FREQ_5                           = 97
	LGW_IF_FREQ_6                           = 98
	LGW_IF_FREQ_7                           = 99
	LGW_IF_FREQ_8                           = 100
	LGW_IF_FREQ_9                           = 101
	LGW_CHANN_OVERRIDE_AGC_GAIN             = 102
	LGW_CHANN_AGC_GAIN                      = 103
	LGW_CORR0_DETECT_EN                     = 104
	LGW_CORR1_DETECT_EN                     = 105
	LGW_CORR2_DETECT_EN                     = 106
	LGW_CORR3_DETECT_EN                     = 107
	LGW_CORR4_DETECT_EN                     = 108
	LGW_CORR5_DETECT_EN                     = 109
	LGW_CORR6_DETECT_EN                     = 110
	LGW_CORR7_DETECT_EN                     = 111
	LGW_CORR_SAME_PEAKS_OPTION_SF6          = 112
	LGW_CORR_SAME_PEAKS_OPTION_SF7          = 113
	LGW_CORR_SAME_PEAKS_OPTION_SF8          = 114
	LGW_CORR_SAME_PEAKS_OPTION_SF9          = 115
	LGW_CORR_SAME_PEAKS_OPTION_SF10         = 116
	LGW_CORR_SAME_PEAKS_OPTION_SF11         = 117
	LGW_CORR_SAME_PEAKS_OPTION_SF12         = 118
	LGW_CORR_SIG_NOISE_RATIO_SF6            = 119
	LGW_CORR_SIG_NOISE_RATIO_SF7            = 120
	LGW_CORR_SIG_NOISE_RATIO_SF8            = 121
	LGW_CORR_SIG_NOISE_RATIO_SF9            = 122
	LGW_CORR_SIG_NOISE_RATIO_SF10           = 123
	LGW_CORR_SIG_NOISE_RATIO_SF11           = 124
	LGW_CORR_SIG_NOISE_RATIO_SF12           = 125
	LGW_CORR_NUM_SAME_PEAK                  = 126
	LGW_CORR_MAC_GAIN                       = 127
	LGW_ADJUST_MODEM_START_OFFSET_RDX4      = 128
	LGW_ADJUST_MODEM_START_OFFSET_SF12_RDX4 = 129
	LGW_DBG_CORR_SELECT_SF                  = 130
	LGW_DBG_CORR_SELECT_CHANNEL             = 131
	LGW_DBG_DETECT_CPT                      = 132
	LGW_DBG_SYMB_CPT                        = 133
	LGW_CHIRP_INVERT_RX                     = 134
	LGW_DC_NOTCH_EN                         = 135
	LGW_IMPLICIT_CRC_EN                     = 136
	LGW_IMPLICIT_CODING_RATE                = 137
	LGW_IMPLICIT_PAYLOAD_LENGHT             = 138
	LGW_FREQ_TO_TIME_INVERT                 = 139
	LGW_FREQ_TO_TIME_DRIFT                  = 140
	LGW_PAYLOAD_FINE_TIMING_GAIN            = 141
	LGW_PREAMBLE_FINE_TIMING_GAIN           = 142
	LGW_TRACKING_INTEGRAL                   = 143
	LGW_FRAME_SYNCH_PEAK1_POS               = 144
	LGW_FRAME_SYNCH_PEAK2_POS               = 145
	LGW_PREAMBLE_SYMB1_NB                   = 146
	LGW_FRAME_SYNCH_GAIN                    = 147
	LGW_SYNCH_DETECT_TH                     = 148
	LGW_LLR_SCALE                           = 149
	LGW_SNR_AVG_CST                         = 150
	LGW_PPM_OFFSET                          = 151
	LGW_MAX_PAYLOAD_LEN                     = 152
	LGW_ONLY_CRC_EN                         = 153
	LGW_ZERO_PAD                            = 154
	LGW_DEC_GAIN_OFFSET                     = 155
	LGW_CHAN_GAIN_OFFSET                    = 156
	LGW_FORCE_HOST_RADIO_CTRL               = 157
	LGW_FORCE_HOST_FE_CTRL                  = 158
	LGW_FORCE_DEC_FILTER_GAIN               = 159
	LGW_MCU_RST_0                           = 160
	LGW_MCU_RST_1                           = 161
	LGW_MCU_SELECT_MUX_0                    = 162
	LGW_MCU_SELECT_MUX_1                    = 163
	LGW_MCU_CORRUPTION_DETECTED_0           = 164
	LGW_MCU_CORRUPTION_DETECTED_1           = 165
	LGW_MCU_SELECT_EDGE_0                   = 166
	LGW_MCU_SELECT_EDGE_1                   = 167
	LGW_CHANN_SELECT_RSSI                   = 168
	LGW_RSSI_BB_DEFAULT_VALUE               = 169
	LGW_RSSI_DEC_DEFAULT_VALUE              = 170
	LGW_RSSI_CHANN_DEFAULT_VALUE            = 171
	LGW_RSSI_BB_FILTER_ALPHA                = 172
	LGW_RSSI_DEC_FILTER_ALPHA               = 173
	LGW_RSSI_CHANN_FILTER_ALPHA             = 174
	LGW_IQ_MISMATCH_A_AMP_COEFF             = 175
	LGW_IQ_MISMATCH_A_PHI_COEFF             = 176
	LGW_IQ_MISMATCH_B_AMP_COEFF             = 177
	LGW_IQ_MISMATCH_B_SEL_I                 = 178
	LGW_IQ_MISMATCH_B_PHI_COEFF             = 179
	LGW_TX_TRIG_IMMEDIATE                   = 180
	LGW_TX_TRIG_DELAYED                     = 181
	LGW_TX_TRIG_GPS                         = 182
	LGW_TX_START_DELAY                      = 183
	LGW_TX_FRAME_SYNCH_PEAK1_POS            = 184
	LGW_TX_FRAME_SYNCH_PEAK2_POS            = 185
	LGW_TX_RAMP_DURATION                    = 186
	LGW_TX_OFFSET_I                         = 187
	LGW_TX_OFFSET_Q                         = 188
	LGW_TX_MODE                             = 189
	LGW_TX_ZERO_PAD                         = 190
	LGW_TX_EDGE_SELECT                      = 191
	LGW_TX_EDGE_SELECT_TOP                  = 192
	LGW_TX_GAIN                             = 193
	LGW_TX_CHIRP_LOW_PASS                   = 194
	LGW_TX_FCC_WIDEBAND                     = 195
	LGW_TX_SWAP_IQ                          = 196
	LGW_MBWSSF_IMPLICIT_HEADER              = 197
	LGW_MBWSSF_IMPLICIT_CRC_EN              = 198
	LGW_MBWSSF_IMPLICIT_CODING_RATE         = 199
	LGW_MBWSSF_IMPLICIT_PAYLOAD_LENGHT      = 200
	LGW_MBWSSF_AGC_FREEZE_ON_DETECT         = 201
	LGW_MBWSSF_FRAME_SYNCH_PEAK1_POS        = 202
	LGW_MBWSSF_FRAME_SYNCH_PEAK2_POS        = 203
	LGW_MBWSSF_PREAMBLE_SYMB1_NB            = 204
	LGW_MBWSSF_FRAME_SYNCH_GAIN             = 205
	LGW_MBWSSF_SYNCH_DETECT_TH              = 206
	LGW_MBWSSF_DETECT_MIN_SINGLE_PEAK       = 207
	LGW_MBWSSF_DETECT_TRIG_SAME_PEAK_NB     = 208
	LGW_MBWSSF_FREQ_TO_TIME_INVERT          = 209
	LGW_MBWSSF_FREQ_TO_TIME_DRIFT           = 210
	LGW_MBWSSF_PPM_CORRECTION               = 211
	LGW_MBWSSF_PAYLOAD_FINE_TIMING_GAIN     = 212
	LGW_MBWSSF_PREAMBLE_FINE_TIMING_GAIN    = 213
	LGW_MBWSSF_TRACKING_INTEGRAL            = 214
	LGW_MBWSSF_ZERO_PAD                     = 215
	LGW_MBWSSF_MODEM_BW                     = 216
	LGW_MBWSSF_RADIO_SELECT                 = 217
	LGW_MBWSSF_RX_CHIRP_INVERT              = 218
	LGW_MBWSSF_LLR_SCALE                    = 219
	LGW_MBWSSF_SNR_AVG_CST                  = 220
	LGW_MBWSSF_PPM_OFFSET                   = 221
	LGW_MBWSSF_RATE_SF                      = 222
	LGW_MBWSSF_ONLY_CRC_EN                  = 223
	LGW_MBWSSF_MAX_PAYLOAD_LEN              = 224
	LGW_TX_STATUS                           = 225
	LGW_FSK_CH_BW_EXPO                      = 226
	LGW_FSK_RSSI_LENGTH                     = 227
	LGW_FSK_RX_INVERT                       = 228
	LGW_FSK_PKT_MODE                        = 229
	LGW_FSK_PSIZE                           = 230
	LGW_FSK_CRC_EN                          = 231
	LGW_FSK_DCFREE_ENC                      = 232
	LGW_FSK_CRC_IBM                         = 233
	LGW_FSK_ERROR_OSR_TOL                   = 234
	LGW_FSK_RADIO_SELECT                    = 235
	LGW_FSK_BR_RATIO                        = 236
	LGW_FSK_REF_PATTERN_LSB                 = 237
	LGW_FSK_REF_PATTERN_MSB                 = 238
	LGW_FSK_PKT_LENGTH                      = 239
	LGW_FSK_TX_GAUSSIAN_EN                  = 240
	LGW_FSK_TX_GAUSSIAN_SELECT_BT           = 241
	LGW_FSK_TX_PATTERN_EN                   = 242
	LGW_FSK_TX_PREAMBLE_SEQ                 = 243
	LGW_FSK_TX_PSIZE                        = 244
	LGW_FSK_NODE_ADRS                       = 245
	LGW_FSK_BROADCAST                       = 246
	LGW_FSK_AUTO_AFC_ON                     = 247
	LGW_FSK_PATTERN_TIMEOUT_CFG             = 248
	LGW_SPI_RADIO_A__DATA                   = 249
	LGW_SPI_RADIO_A__DATA_READBACK          = 250
	LGW_SPI_RADIO_A__ADDR                   = 251
	LGW_SPI_RADIO_A__CS                     = 252
	LGW_SPI_RADIO_B__DATA                   = 253
	LGW_SPI_RADIO_B__DATA_READBACK          = 254
	LGW_SPI_RADIO_B__ADDR                   = 255
	LGW_SPI_RADIO_B__CS                     = 256
	LGW_RADIO_A_EN                          = 257
	LGW_RADIO_B_EN                          = 258
	LGW_RADIO_RST                           = 259
	LGW_LNA_A_EN                            = 260
	LGW_PA_A_EN                             = 261
	LGW_LNA_B_EN                            = 262
	LGW_PA_B_EN                             = 263
	LGW_PA_GAIN                             = 264
	LGW_LNA_A_CTRL_LUT                      = 265
	LGW_PA_A_CTRL_LUT                       = 266
	LGW_LNA_B_CTRL_LUT                      = 267
	LGW_PA_B_CTRL_LUT                       = 268
	LGW_CAPTURE_SOURCE                      = 269
	LGW_CAPTURE_START                       = 270
	LGW_CAPTURE_FORCE_TRIGGER               = 271
	LGW_CAPTURE_WRAP                        = 272
	LGW_CAPTURE_PERIOD                      = 273
	LGW_MODEM_STATUS                        = 274
	LGW_VALID_HEADER_COUNTER_0              = 275
	LGW_VALID_PACKET_COUNTER_0              = 276
	LGW_VALID_HEADER_COUNTER_MBWSSF         = 277
	LGW_VALID_HEADER_COUNTER_FSK            = 278
	LGW_VALID_PACKET_COUNTER_MBWSSF         = 279
	LGW_VALID_PACKET_COUNTER_FSK            = 280
	LGW_CHANN_RSSI                          = 281
	LGW_BB_RSSI                             = 282
	LGW_DEC_RSSI                            = 283
	LGW_DBG_MCU_DATA                        = 284
	LGW_DBG_ARB_MCU_RAM_DATA                = 285
	LGW_DBG_AGC_MCU_RAM_DATA                = 286
	LGW_NEXT_PACKET_CNT                     = 287
	LGW_ADDR_CAPTURE_COUNT                  = 288
	LGW_TIMESTAMP                           = 289
	LGW_DBG_CHANN0_GAIN                     = 290
	LGW_DBG_CHANN1_GAIN                     = 291
	LGW_DBG_CHANN2_GAIN                     = 292
	LGW_DBG_CHANN3_GAIN                     = 293
	LGW_DBG_CHANN4_GAIN                     = 294
	LGW_DBG_CHANN5_GAIN                     = 295
	LGW_DBG_CHANN6_GAIN                     = 296
	LGW_DBG_CHANN7_GAIN                     = 297
	LGW_DBG_DEC_FILT_GAIN                   = 298
	LGW_SPI_DATA_FIFO_PTR                   = 299
	LGW_PACKET_DATA_FIFO_PTR                = 300
	LGW_DBG_ARB_MCU_RAM_ADDR                = 301
	LGW_DBG_AGC_MCU_RAM_ADDR                = 302
	LGW_SPI_MASTER_CHIP_SELECT_POLARITY     = 303
	LGW_SPI_MASTER_CPOL                     = 304
	LGW_SPI_MASTER_CPHA                     = 305
	LGW_SIG_GEN_ANALYSER_MUX_SEL            = 306
	LGW_SIG_GEN_EN                          = 307
	LGW_SIG_ANALYSER_EN                     = 308
	LGW_SIG_ANALYSER_AVG_LEN                = 309
	LGW_SIG_ANALYSER_PRECISION              = 310
	LGW_SIG_ANALYSER_VALID_OUT              = 311
	LGW_SIG_GEN_FREQ                        = 312
	LGW_SIG_ANALYSER_FREQ                   = 313
	LGW_SIG_ANALYSER_I_OUT                  = 314
	LGW_SIG_ANALYSER_Q_OUT                  = 315
	LGW_GPS_EN                              = 316
	LGW_GPS_POL                             = 317
	LGW_SW_TEST_REG1                        = 318
	LGW_SW_TEST_REG2                        = 319
	LGW_SW_TEST_REG3                        = 320
	LGW_DATA_MNGT_STATUS                    = 321
	LGW_DATA_MNGT_CPT_FRAME_ALLOCATED       = 322
	LGW_DATA_MNGT_CPT_FRAME_FINISHED        = 323
	LGW_DATA_MNGT_CPT_FRAME_READEN          = 324
	LGW_TX_TRIG_ALL                         = 325
)
