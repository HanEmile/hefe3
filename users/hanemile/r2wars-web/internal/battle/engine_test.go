package battle

import (
	"fmt"
	"strings"
	"testing"
)

// fakePipe is a scripted stand-in for a real r2 process: it lets engine
// tests exercise the full round loop - including more than two bots and a
// mid-battle death - without radare2 installed.
type fakePipe struct {
	// theEndAfter is the 1-indexed call number of "?v theend" that should
	// start returning "0x1" (someone died). 0 means never.
	theEndAfter int
	theEndCalls int

	closed bool
	cmds   []string
}

func (f *fakePipe) Cmd(input string) (string, error) {
	f.cmds = append(f.cmds, input)

	if input == "?v theend" {
		f.theEndCalls++
		if f.theEndAfter != 0 && f.theEndCalls >= f.theEndAfter {
			return "0x1", nil
		}
		return "0x0", nil
	}

	if strings.HasPrefix(input, "rasm2") {
		return "90909090", nil // fake bytecode
	}

	return "ok", nil
}

func (f *fakePipe) Close() error {
	f.closed = true
	return nil
}

func specs(n int) []Spec {
	out := make([]Spec, n)
	for i := range out {
		out[i] = Spec{
			Name:     fmt.Sprintf("bot-%d", i),
			Source:   "nop",
			ArchName: "x86",
			BitsName: "32",
		}
	}
	return out
}

func TestRunRejectsEmptyBotList(t *testing.T) {
	p := &fakePipe{}
	if _, err := Run(p, 4096, 10, nil); err == nil {
		t.Fatal("Run() with no bots: want error, got nil")
	}
}

func TestRunRejectsNonPositiveArena(t *testing.T) {
	p := &fakePipe{}
	if _, err := Run(p, 0, 10, specs(2)); err == nil {
		t.Fatal("Run() with zero arena size: want error, got nil")
	}
}

// TestRunSupportsMoreThanTwoBots is a regression test for the original HTTP
// handler, which hardcoded a 2-element runtime bot array and panicked with
// an index-out-of-range as soon as a battle had a 3rd participant.
func TestRunSupportsMoreThanTwoBots(t *testing.T) {
	p := &fakePipe{}
	outcome, err := Run(p, 4096, 6, specs(3))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Rounds != 6 {
		t.Fatalf("outcome.Rounds = %d, want 6 (ran to maxRounds, nobody died)", outcome.Rounds)
	}
	if outcome.DiedBotIndex != -1 {
		t.Fatalf("outcome.DiedBotIndex = %d, want -1", outcome.DiedBotIndex)
	}

	// every bot should have had its source assembled exactly once
	asmCount := 0
	for _, c := range p.cmds {
		if strings.HasPrefix(c, "rasm2") && strings.Contains(c, "-D") == false {
			asmCount++
		}
	}
	if asmCount != 3 {
		t.Fatalf("assembled %d bots, want 3", asmCount)
	}
}

// TestRunStopsEarlyOnDeath is a regression test for the original loop, which
// logged a bot death but kept simulating all the way to MaxRounds regardless.
func TestRunStopsEarlyOnDeath(t *testing.T) {
	p := &fakePipe{theEndAfter: 2} // die on the 2nd status check
	outcome, err := Run(p, 4096, 100, specs(2))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Rounds >= 100 {
		t.Fatalf("outcome.Rounds = %d, want well under MaxRounds (100) since a bot died", outcome.Rounds)
	}
	if outcome.DiedBotIndex == -1 {
		t.Fatal("outcome.DiedBotIndex = -1, want the index of the bot that died")
	}
}

func TestRunPropagatesAssemblyFailure(t *testing.T) {
	p := &failingPipe{failOn: "rasm2"}
	if _, err := Run(p, 4096, 10, specs(2)); err == nil {
		t.Fatal("Run() with failing assembly: want error, got nil")
	}
}

type failingPipe struct {
	failOn string
}

func (f *failingPipe) Cmd(input string) (string, error) {
	if strings.Contains(input, f.failOn) {
		return "", fmt.Errorf("simulated failure running %q", input)
	}
	return "ok", nil
}

func (f *failingPipe) Close() error { return nil }
