// Package battle runs a r2wars simulation: assembling each bot, loading
// them into a shared arena, and round-robin single-stepping each one until
// an end condition fires or the round limit is hit.
//
// The engine only depends on the small r2client.Pipe interface, not a
// concrete r2 process, so the whole simulation loop - including the
// previously untested "more than two bots" and "a bot dies mid-battle"
// paths - can run under `go test` without radare2 installed.
package battle

import (
	"fmt"
	"strings"

	"git.emile.space/r2wars-web/internal/r2client"
)

// Spec describes one bot entering the arena.
type Spec struct {
	Name     string
	Source   string
	ArchName string
	BitsName string
}

// runtimeBot tracks the per-bot state threaded through the round loop. The
// original implementation hardcoded a 2-element array of this shape, which
// panicked with index-out-of-range as soon as a 3rd bot was submitted to a
// battle; this is now sized to len(bots).
type runtimeBot struct {
	Spec
	BaseAddr int
	Regs     string
}

// Outcome describes how a battle ended.
type Outcome struct {
	RawOutput string
	Rounds    int
	// DiedBotIndex is the index into the input Specs slice of the bot that
	// triggered the end condition (trap/segfault/illegal instruction/...),
	// or -1 if the battle ran to MaxRounds without anyone dying.
	DiedBotIndex int
}

// Run assembles bots, loads them into a malloc:// arena of arenaSize bytes,
// and single-steps them round-robin for up to maxRounds rounds, stopping
// early the moment any bot hits an end condition.
//
// It requires at least one bot and a positive arena size; r2 is the already
// connected pipe to run commands against (its lifecycle is owned by the
// caller).
func Run(r2 r2client.Pipe, arenaSize, maxRounds int, bots []Spec) (Outcome, error) {
	if len(bots) == 0 {
		return Outcome{}, fmt.Errorf("battle: need at least one bot")
	}
	if arenaSize <= 0 {
		return Outcome{}, fmt.Errorf("battle: arena size must be positive, got %d", arenaSize)
	}

	var out strings.Builder
	logf := func(format string, args ...interface{}) {
		fmt.Fprintf(&out, format, args...)
	}

	run := func(cmd string) (string, error) {
		result, err := r2client.Cmd(r2, cmd)
		logf("[0x00000000]> %s\n%s\n", cmd, result)
		return result, err
	}

	if _, err := run(fmt.Sprintf("pxc %d @ 0x0", arenaSize)); err != nil {
		return Outcome{}, err
	}

	runtime := make([]runtimeBot, len(bots))
	bytecode := make([]string, len(bots))
	for i, b := range bots {
		runtime[i].Spec = b

		src := strings.ReplaceAll(b.Source, "\r\n", "; ")
		asmCmd := fmt.Sprintf("rasm2 -a %s -b %s \"%s\"", b.ArchName, b.BitsName, src)
		asm, err := run(asmCmd)
		if err != nil {
			return Outcome{}, fmt.Errorf("battle: assembling bot %q: %w", b.Name, err)
		}
		bytecode[i] = asm
	}

	if _, err := run("aei"); err != nil {
		return Outcome{}, err
	}
	if _, err := run("aeim"); err != nil {
		return Outcome{}, err
	}

	// place bots: each gets a distinct base address, program counter and
	// stack pointer, then has its initial register state snapshotted so the
	// round loop can swap contexts between bots.
	for i := range runtime {
		addr := 50 * (i + 1)
		runtime[i].BaseAddr = addr

		if _, err := run(fmt.Sprintf("wx %s @ 0x%d", bytecode[i], addr)); err != nil {
			return Outcome{}, fmt.Errorf("battle: writing bot %q: %w", runtime[i].Name, err)
		}
		if _, err := run(fmt.Sprintf("aer PC=0x%d", addr)); err != nil {
			return Outcome{}, err
		}
		if _, err := run(fmt.Sprintf("aer SP=SP+0x%d", addr)); err != nil {
			return Outcome{}, err
		}

		regs, err := run("aerR")
		if err != nil {
			return Outcome{}, err
		}
		runtime[i].Regs = strings.ReplaceAll(regs, "\n", ";")
	}

	for i := range runtime {
		if _, err := run(fmt.Sprintf("pxc 100 @ 0x%d", runtime[i].BaseAddr)); err != nil {
			return Outcome{}, err
		}
	}

	// end conditions: any of these flips the `theend` flag, which is polled
	// after every single step below.
	for _, cmd := range []string{
		"e cmd.esil.todo=t theend=1",
		"e cmd.esil.trap=t theend=1",
		"e cmd.esil.intr=t theend=1",
		"e cmd.esil.ioer=t theend=1",
		"f theend=0",
	} {
		if _, err := run(cmd); err != nil {
			return Outcome{}, err
		}
	}

	diedBotIndex := -1
	round := 0
	for ; round < maxRounds; round++ {
		currentBotID := round % len(runtime)
		current := &runtime[currentBotID]

		logf("[0x00000000]> ########################################################################\n")

		if _, err := run(current.Regs); err != nil {
			return Outcome{}, err
		}

		pc, _ := run("aer~$(arn PC)~[1]")
		arch, _ := run("e asm.arch")
		bits, _ := run("e asm.bits")
		logf("[0x00000000]> # ROUND %d, BOT %d (%s), PC=%s, arch=%s, bits=%s\n", round, currentBotID, current.Name, pc, arch, bits)

		if _, err := run(fmt.Sprintf("e asm.arch=%s", current.ArchName)); err != nil {
			return Outcome{}, err
		}
		if _, err := run(fmt.Sprintf("e asm.bits=%s", current.BitsName)); err != nil {
			return Outcome{}, err
		}

		if _, err := run("aes"); err != nil {
			return Outcome{}, err
		}

		registers, err := run("aerR")
		if err != nil {
			return Outcome{}, err
		}
		current.Regs = strings.ReplaceAll(registers, "\n", ";")

		if _, err := run(fmt.Sprintf("pxc 100 @ 0x%d", current.BaseAddr)); err != nil {
			return Outcome{}, err
		}

		status, err := run("?v theend")
		if err != nil {
			return Outcome{}, err
		}
		switch strings.TrimSpace(status) {
		case "0x1":
			logf("[!] Bot %d (%s) triggered the end condition\n", currentBotID, current.Name)
			diedBotIndex = currentBotID
		case "0x0":
			// still running
		default:
			logf("[!] Got invalid status %q for bot %d (%s)\n", strings.TrimSpace(status), currentBotID, current.Name)
		}

		if diedBotIndex != -1 {
			round++
			break
		}
	}

	return Outcome{
		RawOutput:    out.String(),
		Rounds:       round,
		DiedBotIndex: diedBotIndex,
	}, nil
}
