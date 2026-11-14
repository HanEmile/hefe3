package com

import (
	"bytes"
	serial "go.bug.st/serial"
	"sync"
	"testing"
	"time"
)

// fakePort implements a programmable serial.Port double: it feeds scripted
// bytes into Read and captures everything written.
type fakePort struct {
	mu      sync.Mutex
	inbuf   []byte
	written bytes.Buffer
}

func (p *fakePort) feed(b []byte) {
	p.mu.Lock()
	p.inbuf = append(p.inbuf, b...)
	p.mu.Unlock()
}

func (p *fakePort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := copy(b, p.inbuf)
	p.inbuf = p.inbuf[n:]
	time.Sleep(time.Millisecond)
	return n, nil
}

func (p *fakePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Write(b)
}

func (p *fakePort) Close() error                       { return nil }
func (p *fakePort) Break(time.Duration) error          { return nil }
func (p *fakePort) Drain() error                       { return nil }
func (p *fakePort) ResetInputBuffer() error            { return nil }
func (p *fakePort) ResetOutputBuffer() error           { return nil }
func (p *fakePort) SetDTR(bool) error                  { return nil }
func (p *fakePort) SetRTS(bool) error                  { return nil }
func (p *fakePort) SetReadTimeout(time.Duration) error { return nil }
func (p *fakePort) SetMode(*serial.Mode) error         { return nil }
func (p *fakePort) GetModemStatusBits() (bits *serial.ModemStatusBits, err error) {
	return nil, nil
}

func openFake(p *fakePort) *Bridge { return &Bridge{port: p} }

func TestCommandRoundTrip(t *testing.T) {
	f := &fakePort{}
	b := openFake(f)
	go func() {
		// allow the command to be written first
		time.Sleep(20 * time.Millisecond)
		f.feed([]byte{'w', 0, 0, 1})
	}()
	if err := b.W(0x33, 0xAB); err != nil {
		t.Fatal(err)
	}
	want := []byte{'w', 0, 1, 0x33, 0xAB}
	if !bytes.Equal(f.written.Bytes(), want) {
		t.Fatalf("wrote %v, want %v", f.written.Bytes(), want)
	}
}

func TestResync(t *testing.T) {
	f := &fakePort{}
	b := openFake(f)
	go func() {
		time.Sleep(20 * time.Millisecond)
		// garbage before the real answer
		f.feed([]byte{0x00, 0xFF, 'r', 0, 1, 1, 0x42})
	}()
	v, err := b.R(0x01)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0x42 {
		t.Fatalf("read %d, want 0x42", v)
	}
}

func TestWbChunking(t *testing.T) {
	f := &fakePort{}
	b := openFake(f)
	go func() {
		time.Sleep(10 * time.Millisecond)
		// 'x' first, 'y' middle, 'z' end for 1250 bytes (600+600+50)
		f.feed([]byte{'x', 0, 0, 1})
		f.feed([]byte{'y', 0, 0, 1})
		f.feed([]byte{'z', 0, 0, 1})
	}()
	if err := b.Wb(0x07, make([]byte, 1250)); err != nil {
		t.Fatal(err)
	}
	w := f.written.Bytes()
	if w[0] != 'x' || w[604] != 'y' || w[1208] != 'z' {
		t.Fatalf("bad chunk ids: %q %q %q", w[0], w[604], w[1208])
	}
	// last chunk length: 50
	if w[1209] != 0 || w[1210] != 50 {
		t.Fatalf("bad end chunk len: %v", w[1208:1212])
	}
}

func TestRbChunking(t *testing.T) {
	f := &fakePort{}
	b := openFake(f)
	go func() {
		time.Sleep(10 * time.Millisecond)
		// 1000 bytes read: 's' 900 + 'u' 100
		ans1 := append([]byte{'s', 3, 132, 1}, bytes.Repeat([]byte{0xAA}, 900)...)
		ans2 := append([]byte{'u', 0, 100, 1}, bytes.Repeat([]byte{0xBB}, 100)...)
		f.feed(ans1)
		f.feed(ans2)
	}()
	out, err := b.Rb(0x07, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1000 {
		t.Fatalf("got %d bytes", len(out))
	}
	if out[0] != 0xAA || out[899] != 0xAA || out[900] != 0xBB || out[999] != 0xBB {
		t.Fatal("content mismatch")
	}
	w := f.written.Bytes()
	// first cmd: 's' 0 2 0x07 0x03 0x84
	if !bytes.Equal(w[:6], []byte{'s', 0, 2, 0x07, 3, 132}) {
		t.Fatalf("bad first read cmd: %v", w[:6])
	}
	if !bytes.Equal(w[6:], []byte{'u', 0, 2, 0x07, 0, 100}) {
		t.Fatalf("bad end read cmd: %v", w[6:])
	}
}
