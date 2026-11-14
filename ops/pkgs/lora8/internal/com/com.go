// Package com implements the Semtech PicoCell GW "com" protocol spoken by the
// STM32 USB->SPI bridge found on the MikroTik R11e-LR8 (and Semtech PicoCell
// GW reference boards). It is a Go port of loragw_com_linux.c from
// Lora-net/picoGW_hal.
//
// Wire format (all multi-byte integers big-endian):
//
//	host -> MCU:  [ cmdID(1) | len_msb(1) | len_lsb(1) | address(1) | data... ]
//	MCU  -> host: [ cmdID(1) | len_msb(1) | len_lsb(1) | status(1)   | data... ]
package com

import (
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"
)

const (
	atomicTX = 600
	atomicRX = 900

	cmdHeaderSize = 4
	maxCmdDataTX  = atomicTX
	maxAnsData    = 1024 + 16*44 // MAX_FIFO + 16 * METADATA_SIZE_ALIGNED

	ackOK = 1
)

var validCmds = [256]bool{}

func init() {
	for _, c := range []byte("rstupwxyzabcdfhqijlmn") {
		validCmds[c] = true
	}
}

// Bridge is a handle to the USB MCU bridge.
type Bridge struct {
	port serial.Port
}

// Answer holds a decoded MCU answer frame.
type Answer struct {
	ID     byte
	Status byte
	Data   []byte
}

// Open opens the CDC-ACM device at path (115200 8N1 raw) and returns a Bridge.
func Open(path string) (*Bridge, error) {
	port, err := serial.Open(path, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := port.SetReadTimeout(time.Second); err != nil {
		port.Close()
		return nil, err
	}
	// flush anything left over from a previous session
	port.ResetInputBuffer()
	return &Bridge{port: port}, nil
}

// Close releases the serial port.
func (b *Bridge) Close() error { return b.port.Close() }

func (b *Bridge) sendCmd(id byte, addr uint8, data []byte) error {
	if len(data) > maxCmdDataTX {
		return fmt.Errorf("command data too long: %d", len(data))
	}
	buf := make([]byte, 0, cmdHeaderSize+len(data))
	buf = append(buf, id, byte(len(data)>>8), byte(len(data)), addr)
	buf = append(buf, data...)
	if _, err := b.port.Write(buf); err != nil {
		return fmt.Errorf("write cmd %q: %w", id, err)
	}
	return nil
}

func (b *Bridge) readFull(buf []byte, deadline time.Time) (int, error) {
	n := 0
	for n < len(buf) {
		if time.Now().After(deadline) {
			return n, errors.New("read timeout")
		}
		m, err := b.port.Read(buf[n:])
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

func (b *Bridge) receiveAns() (*Answer, error) {
	deadline := time.Now().Add(3 * time.Second)
	hdr := make([]byte, cmdHeaderSize)

	// resync: read byte-wise until the first byte looks like a valid cmd id
	for !validCmds[hdr[0]] {
		if _, err := b.readFull(hdr[:1], deadline); err != nil {
			return nil, fmt.Errorf("waiting for answer header: %w", err)
		}
	}
	if _, err := b.readFull(hdr[1:], deadline); err != nil {
		return nil, fmt.Errorf("reading answer header: %w", err)
	}

	size := int(hdr[1])<<8 | int(hdr[2])
	readSize := size
	if readSize > 0 {
		if (readSize+cmdHeaderSize)%64 == 0 {
			readSize++ // USB driver pads full 64-byte frames
		}
		if readSize > maxAnsData {
			return nil, fmt.Errorf("answer too long: %d", readSize)
		}
	}
	data := make([]byte, readSize)
	if _, err := b.readFull(data, deadline); err != nil {
		return nil, fmt.Errorf("reading answer data: %w", err)
	}
	return &Answer{ID: hdr[0], Status: hdr[3], Data: data}, nil
}

// Command sends a raw MCU command and waits for its answer.
func (b *Bridge) Command(id byte, addr uint8, data []byte) (*Answer, error) {
	if err := b.sendCmd(id, addr, data); err != nil {
		return nil, err
	}
	ans, err := b.receiveAns()
	if err != nil {
		return nil, err
	}
	if ans.ID != id {
		return ans, fmt.Errorf("answer id %q does not match cmd %q", ans.ID, id)
	}
	return ans, nil
}

// W writes a single SX1301 register byte.
func (b *Bridge) W(addr uint8, data uint8) error {
	_, err := b.Command('w', addr, []byte{data})
	return err
}

// R reads a single SX1301 register byte.
func (b *Bridge) R(addr uint8) (uint8, error) {
	ans, err := b.Command('r', addr, []byte{0})
	if err != nil {
		return 0, err
	}
	if len(ans.Data) < 1 {
		return 0, errors.New("register read returned no data")
	}
	return ans.Data[0], nil
}

// Wb does a burst write of arbitrary size (chunked per the bridge protocol).
func (b *Bridge) Wb(addr uint8, data []byte) error {
	if len(data) == 0 {
		return errors.New("burst of null length")
	}
	sent := 0
	left := len(data)
	for left > atomicTX {
		var id byte
		if left == len(data) {
			id = 'x' // first chunk
		} else {
			id = 'y' // middle chunk
		}
		if err := b.sendCmd(id, addr, data[sent:sent+atomicTX]); err != nil {
			return err
		}
		if _, err := b.receiveAns(); err != nil {
			return err
		}
		sent += atomicTX
		left -= atomicTX
	}
	id := byte('z') // end chunk
	if len(data) <= atomicTX {
		id = 'a' // atomic transfer
	}
	if err := b.sendCmd(id, addr, data[sent:]); err != nil {
		return err
	}
	_, err := b.receiveAns()
	return err
}

// Rb does a burst read of arbitrary size.
func (b *Bridge) Rb(addr uint8, size int) ([]byte, error) {
	if size <= 0 {
		return nil, errors.New("burst of null length")
	}
	out := make([]byte, 0, size)
	left := size
	for left > atomicRX {
		var id byte
		if left == size {
			id = 's' // first chunk
		} else {
			id = 't' // middle chunk
		}
		sz := uint16(atomicRX)
		ans, err := b.Command(id, addr, []byte{byte(sz >> 8), byte(sz)})
		if err != nil {
			return nil, err
		}
		if len(ans.Data) < atomicRX {
			return nil, fmt.Errorf("short burst read: %d", len(ans.Data))
		}
		out = append(out, ans.Data[:atomicRX]...)
		left -= atomicRX
	}
	id := byte('u') // end chunk
	if size <= atomicRX {
		id = 'p' // atomic transfer
	}
	ans, err := b.Command(id, addr, []byte{byte(left >> 8), byte(left)})
	if err != nil {
		return nil, err
	}
	if len(ans.Data) < left {
		return nil, fmt.Errorf("short burst read: %d", len(ans.Data))
	}
	return append(out, ans.Data[:left]...), nil
}
