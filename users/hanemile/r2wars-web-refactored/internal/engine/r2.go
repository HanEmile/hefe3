// Package engine runs Core War-style battles: it assembles radare2 assembly
// "bots" into bytecode, lays them out in a malloc:// arena and steps each bot
// one ESIL instruction at a time, turn by turn, until at most one bot survives
// or the round budget is exhausted.
//
// The original implementation lived inlined in an HTTP handler and was
// hardcoded to exactly two bots. This package extracts that logic, generalises
// it to N bots and hides radare2 behind the small R2 interface so the round
// logic and the DSL parser can be unit-tested without r2 installed.
package engine

import (
	"fmt"
	"log"
	"strings"

	r2pipe "github.com/radareorg/r2pipe-go"
)

// R2 is the minimal radare2 control surface the engine needs. Production code
// uses the r2pipe-backed implementation returned by OpenR2; tests can supply a
// fake.
type R2 interface {
	// Cmd runs a single r2 command and returns its (possibly empty) output.
	Cmd(command string) (string, error)
	// Close releases the underlying r2 process.
	Close() error
}

// Debug, when true, logs every command sent through an r2pipe-backed R2,
// mirroring the original r2cmd helper. It is off by default so the test suite
// and the web layer stay quiet unless they opt in.
var Debug = false

// r2pipeR2 wraps an r2pipe.Pipe to satisfy R2.
type r2pipeR2 struct {
	pipe *r2pipe.Pipe
}

// OpenR2 opens a radare2 pipe on the given uri (e.g. "malloc://4096") and wraps
// it as an R2.
func OpenR2(uri string) (R2, error) {
	pipe, err := r2pipe.NewPipe(uri)
	if err != nil {
		return nil, fmt.Errorf("opening r2pipe on %q: %w", uri, err)
	}
	return &r2pipeR2{pipe: pipe}, nil
}

func (r *r2pipeR2) Cmd(command string) (string, error) {
	if Debug {
		log.Printf("> %s", command)
	}
	out, err := r.pipe.Cmd(command)
	if err != nil {
		if Debug {
			log.Printf("  ! %v", err)
		}
		return "", err
	}
	return out, nil
}

func (r *r2pipeR2) Close() error {
	return r.pipe.Close()
}

// flattenSource turns multi-line assembly into the single-line, "; "-separated
// form rasm2 expects on the command line. Both \r\n and bare \n are handled so
// sources coming from web forms and from files both work.
func flattenSource(source string) string {
	s := strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, "; ")
}

// Assemble assembles source for the given arch/bits and returns the resulting
// hex bytecode, trimmed of surrounding whitespace.
func Assemble(r R2, arch, bits, source string) (string, error) {
	cmd := fmt.Sprintf("rasm2 -a %s -b %s \"%s\"", arch, bits, flattenSource(source))
	out, err := r.Cmd(cmd)
	if err != nil {
		return "", fmt.Errorf("assembling: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Disassemble disassembles hex bytecode for the given arch/bits.
func Disassemble(r R2, arch, bits, hexbytecode string) (string, error) {
	cmd := fmt.Sprintf("rasm2 -a %s -b %s -D %s", arch, bits, strings.TrimSpace(hexbytecode))
	out, err := r.Cmd(cmd)
	if err != nil {
		return "", fmt.Errorf("disassembling: %w", err)
	}
	return strings.TrimSpace(out), nil
}
