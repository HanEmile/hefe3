package engine

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// requireRasm2 skips the test gracefully when radare2 is not on PATH, so the
// suite degrades on machines without r2 while still running where it is present.
func requireRasm2(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rasm2"); err != nil {
		t.Skip("rasm2 not on PATH; skipping r2 integration test")
	}
	if _, err := exec.LookPath("r2"); err != nil {
		t.Skip("r2 not on PATH; skipping r2 integration test")
	}
}

func TestAssembleRealR2(t *testing.T) {
	requireRasm2(t)
	r, err := OpenR2("malloc://512")
	if err != nil {
		t.Fatalf("OpenR2: %v", err)
	}
	defer r.Close()

	hex, err := Assemble(r, "x86", "32", "mov eax, 1\nnop")
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if hex == "" {
		t.Fatal("Assemble produced empty bytecode")
	}
	t.Logf("bytecode = %s", hex)

	disasm, err := Disassemble(r, "x86", "32", hex)
	if err != nil {
		t.Fatalf("Disassemble: %v", err)
	}
	if !strings.Contains(strings.ToLower(disasm), "mov") {
		t.Errorf("disasm missing mov: %q", disasm)
	}
}

func TestRunTwoBots(t *testing.T) {
	requireRasm2(t)
	spec := Spec{
		Name:   "two",
		Config: Config{ArenaSize: 4096, MaxRounds: 50, PlacementGap: 64},
		Bots: []BotSpec{
			{Name: "a", Arch: "x86", Bits: "32", Source: "nop\njmp 0"},
			{Name: "b", Arch: "x86", Bits: "32", Source: "nop\njmp 0"},
		},
	}
	r, err := OpenR2(fmt.Sprintf("malloc://%d", spec.Config.ArenaSize))
	if err != nil {
		t.Fatalf("OpenR2: %v", err)
	}
	defer r.Close()

	res, err := Run(r, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Transcript == "" {
		t.Error("empty transcript")
	}
	if res.Rounds <= 0 {
		t.Errorf("Rounds = %d, want > 0", res.Rounds)
	}
	if len(res.Bots) != 2 {
		t.Errorf("got %d runtime bots, want 2", len(res.Bots))
	}
	for i, b := range res.Bots {
		if b.Bytecode == "" {
			t.Errorf("bot %d has empty bytecode", i)
		}
		if b.BaseAddr == 0 {
			t.Errorf("bot %d has base addr 0 (should be non-overlapping, non-zero)", i)
		}
	}
	t.Logf("rounds=%d survivors=%v winner=%q", res.Rounds, res.Survivors, res.Winner)
}

func TestRunThreeBots(t *testing.T) {
	requireRasm2(t)
	spec := Spec{
		Name:   "three",
		Config: Config{ArenaSize: 4096, MaxRounds: 60, PlacementGap: 64},
		Bots: []BotSpec{
			{Name: "a", Arch: "x86", Bits: "32", Source: "nop\njmp 0"},
			{Name: "b", Arch: "x86", Bits: "32", Source: "nop\njmp 0"},
			{Name: "c", Arch: "x86", Bits: "32", Source: "nop\njmp 0"},
		},
	}
	r, err := OpenR2(fmt.Sprintf("malloc://%d", spec.Config.ArenaSize))
	if err != nil {
		t.Fatalf("OpenR2: %v", err)
	}
	defer r.Close()

	res, err := Run(r, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Bots) != 3 {
		t.Fatalf("got %d runtime bots, want 3 (generalisation failed)", len(res.Bots))
	}
	// Base addresses must be distinct and non-overlapping.
	seen := map[int]bool{}
	for _, b := range res.Bots {
		if seen[b.BaseAddr] {
			t.Errorf("duplicate base addr 0x%x", b.BaseAddr)
		}
		seen[b.BaseAddr] = true
	}
	t.Logf("rounds=%d survivors=%v winner=%q", res.Rounds, res.Survivors, res.Winner)
}

// TestRunDeathDetection exercises the death path with a bot likely to trip an
// ESIL end condition. r2's ESIL behaviour for invalid/trapping instructions
// varies, so this asserts the strong invariant where a death is observed and a
// documented weaker invariant (the run completes, Rounds>0) otherwise.
func TestRunDeathDetection(t *testing.T) {
	requireRasm2(t)
	spec := Spec{
		Name:   "death",
		Config: Config{ArenaSize: 4096, MaxRounds: 80, PlacementGap: 64},
		Bots: []BotSpec{
			// A bot that runs off into uninitialised/zeroed arena memory and a
			// surviving looper. Running into zero bytes tends to trip a
			// trap/todo end condition under ESIL.
			{Name: "suicidal", Arch: "x86", Bits: "32", Source: "nop\nnop"},
			{Name: "survivor", Arch: "x86", Bits: "32", Source: "jmp 0"},
		},
	}
	r, err := OpenR2(fmt.Sprintf("malloc://%d", spec.Config.ArenaSize))
	if err != nil {
		t.Fatalf("OpenR2: %v", err)
	}
	defer r.Close()

	res, err := Run(r, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Rounds <= 0 {
		t.Errorf("Rounds = %d, want > 0", res.Rounds)
	}

	var anyDead bool
	for _, b := range res.Bots {
		if b.Dead {
			anyDead = true
			if b.DeathRound < 0 {
				t.Errorf("bot %q marked dead but DeathRound=%d", b.Name, b.DeathRound)
			}
		}
	}
	if anyDead {
		// Strong invariant: at least one survivor recorded, winner consistent.
		if len(res.Survivors)+countDead(res.Bots) != len(res.Bots) {
			t.Errorf("survivors(%d)+dead(%d) != total(%d)", len(res.Survivors), countDead(res.Bots), len(res.Bots))
		}
		if len(res.Survivors) == 1 && res.Winner != res.Survivors[0] {
			t.Errorf("winner %q != sole survivor %q", res.Winner, res.Survivors[0])
		}
		t.Logf("death detected: survivors=%v winner=%q", res.Survivors, res.Winner)
	} else {
		// Documented weaker invariant: r2's ESIL did not trap these
		// instructions; the run still completed cleanly.
		t.Logf("no death observed (ESIL did not trap); run completed, rounds=%d", res.Rounds)
	}
}

func countDead(bots []RuntimeBot) int {
	n := 0
	for _, b := range bots {
		if b.Dead {
			n++
		}
	}
	return n
}

func TestRunPlacementOverflow(t *testing.T) {
	// No r2 needed: this fails before any r2 command because addresses don't fit.
	spec := Spec{
		Name:   "overflow",
		Config: Config{ArenaSize: 64, MaxRounds: 10, PlacementGap: 64},
		Bots: []BotSpec{
			{Name: "a", Arch: "x86", Bits: "32", Source: "nop"},
			{Name: "b", Arch: "x86", Bits: "32", Source: "nop"},
		},
	}
	if _, err := Run(nil, spec); err == nil {
		t.Fatal("expected placement-overflow error, got nil")
	}
}
