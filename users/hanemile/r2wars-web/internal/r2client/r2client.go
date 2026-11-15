// Package r2client wraps radare2 process invocations behind a small
// interface so battle simulation and assembly/disassembly can be tested
// without spawning a real r2 binary, and so a failure to start r2 (missing
// binary, OOM, ...) surfaces as an error instead of crashing the process.
package r2client

import (
	"fmt"

	r2pipe "github.com/radareorg/r2pipe-go"
)

// Pipe is anything that can run an r2 command line and return its output.
// *r2pipe.Pipe satisfies this; tests use a fake.
type Pipe interface {
	Cmd(input string) (string, error)
	Close() error
}

// pipe adapts *r2pipe.Pipe to the Pipe interface (it already matches, this
// just documents the dependency edge).
type pipe struct{ *r2pipe.Pipe }

// Open starts a new radare2 process against target (e.g. "--" for no input,
// or "malloc://4096" for a scratch arena) and returns a handle to it. The
// caller must Close it.
func Open(target string) (Pipe, error) {
	p, err := r2pipe.NewPipe(target)
	if err != nil {
		return nil, fmt.Errorf("r2client: starting r2 against %q: %w", target, err)
	}
	return pipe{p}, nil
}

// Cmd runs a single r2 command and returns its output.
func Cmd(p Pipe, input string) (string, error) {
	out, err := p.Cmd(input)
	if err != nil {
		return "", fmt.Errorf("r2client: running %q: %w", input, err)
	}
	return out, nil
}
