package engine

import (
	"fmt"
	"strings"
)

// RuntimeBot is a bot's mutable state during a battle.
type RuntimeBot struct {
	Name       string
	Arch       string
	Bits       string
	BaseAddr   int
	Regs       string // saved aerR register-restore commands ("; "-joined)
	Bytecode   string // hex bytecode produced by Assemble
	Dead       bool
	DeathRound int // round in which the bot died, or -1 if it survived
}

// Config holds the arena parameters for a battle.
type Config struct {
	ArenaSize    int
	MaxRounds    int
	PlacementGap int // distance in bytes between consecutive bot base addresses
}

// DefaultPlacementGap is used when Config.PlacementGap is <= 0. The original
// engine placed bots 50 bytes apart; we keep a comparable, slightly roomier
// default so small bots do not immediately overwrite each other.
const DefaultPlacementGap = 64

// BotSpec describes one bot to run, decoupled from the model/DB types.
type BotSpec struct {
	Name   string
	Arch   string
	Bits   string
	Source string
}

// Spec is a complete, self-contained battle declaration.
type Spec struct {
	Name   string
	Config Config
	Bots   []BotSpec
}

// Result is the outcome of a battle.
type Result struct {
	Spec       Spec
	Transcript string       // RawOutput-style transcript, rendered as <pre>
	Bots       []RuntimeBot // final per-bot state
	Survivors  []string     // names of bots still alive at the end
	Winner     string       // single survivor's name, or "" on draw/timeout
	Rounds     int          // rounds actually executed
}

// placementGap returns the effective gap, falling back to the default.
func (c Config) placementGap() int {
	if c.PlacementGap <= 0 {
		return DefaultPlacementGap
	}
	return c.PlacementGap
}

// transcript accumulates the user-facing battle log in the same
// "[0x00000000]> <cmd>\n<output>\n" style the original handler produced.
type transcript struct {
	b strings.Builder
}

// cmd runs a command, appends it (and its output) to the transcript and returns
// the trimmed output. Like the original, command errors are tolerated: r2 keeps
// running and the transcript records whatever came back.
func (t *transcript) cmd(r R2, command string) string {
	out, _ := r.Cmd(command)
	t.b.WriteString("[0x00000000]> ")
	t.b.WriteString(command)
	t.b.WriteByte('\n')
	if out != "" {
		t.b.WriteString(out)
		if !strings.HasSuffix(out, "\n") {
			t.b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(out)
}

// comment appends an explanatory "# ..." line to the transcript.
func (t *transcript) comment(format string, args ...any) {
	t.b.WriteString("[0x00000000]> # ")
	fmt.Fprintf(&t.b, format, args...)
	t.b.WriteByte('\n')
}

func (t *transcript) String() string { return t.b.String() }

// Run executes a battle described by spec against the supplied, already-open
// R2 (which the caller is expected to have opened on "malloc://<ArenaSize>").
// The caller owns r and is responsible for closing it.
//
// Generalisation over the original two-bot engine:
//   - the turn order is round-robin over all live bots (round % len(bots)),
//   - each bot is placed at a non-overlapping address derived from PlacementGap,
//   - a bot that triggers the ESIL end condition on its own turn is marked Dead
//     with the round number and skipped thereafter,
//   - the loop stops as soon as <=1 bot remains alive,
//   - Winner is the sole survivor, or "" for a draw / timeout with several
//     bots still standing.
func Run(r R2, spec Spec) (*Result, error) {
	if len(spec.Bots) == 0 {
		return nil, fmt.Errorf("battle %q has no bots", spec.Name)
	}
	gap := spec.Config.placementGap()
	arena := spec.Config.ArenaSize
	if arena <= 0 {
		return nil, fmt.Errorf("battle %q has non-positive arena size %d", spec.Name, arena)
	}

	// Placement strategy: the first bot sits at one gap from address 0 (so PC=0
	// is never a valid start, matching the original 50*(i+1) idea) and each
	// subsequent bot is one gap further along. Every base address must lie
	// inside the arena.
	bots := make([]RuntimeBot, len(spec.Bots))
	for i, bs := range spec.Bots {
		addr := gap * (i + 1)
		if addr >= arena {
			return nil, fmt.Errorf("bot %d (%q) base address 0x%x does not fit in arena of %d bytes (gap=%d); enlarge arena or shrink gap",
				i, bs.Name, addr, arena, gap)
		}
		bots[i] = RuntimeBot{
			Name:       bs.Name,
			Arch:       bs.Arch,
			Bits:       bs.Bits,
			BaseAddr:   addr,
			DeathRound: -1,
		}
	}

	t := &transcript{}

	// Show the (empty) arena, just like the original opener.
	t.cmd(r, fmt.Sprintf("pxc %d @ 0x0", arena))

	// Assemble every bot.
	t.comment("Assembling the bots")
	for i := range bots {
		bs := spec.Bots[i]
		bytecode, err := Assemble(r, bs.Arch, bs.Bits, bs.Source)
		if err != nil {
			return nil, fmt.Errorf("assembling bot %q: %w", bs.Name, err)
		}
		if bytecode == "" {
			return nil, fmt.Errorf("bot %q assembled to empty bytecode (bad source or arch/bits %s/%s)", bs.Name, bs.Arch, bs.Bits)
		}
		bots[i].Bytecode = bytecode
		t.comment("bot %q (%s/%s) -> %s", bs.Name, bs.Arch, bs.Bits, bytecode)
	}

	// Init the ESIL VM and its memory/stack.
	t.comment("initializing the vm and the stack")
	t.cmd(r, "aei")
	t.cmd(r, "aeim")

	// Place each bot, set its PC/SP and snapshot its registers so we can restore
	// the right context before each turn.
	for i := range bots {
		addr := bots[i].BaseAddr

		t.comment("writing bot %d (%q) to 0x%x", i, bots[i].Name, addr)
		t.cmd(r, fmt.Sprintf("wx %s @ 0x%x", bots[i].Bytecode, addr))

		t.comment("setting the program counter and the stack pointer")
		t.cmd(r, fmt.Sprintf("aer PC=0x%x", addr))
		t.cmd(r, fmt.Sprintf("aer SP=SP+0x%x", addr))

		t.comment("storing registers for bot %d", i)
		regs := t.cmd(r, "aerR")
		bots[i].Regs = strings.ReplaceAll(regs, "\n", ";")
	}

	// End-condition hooks: any todo/trap/intr/ioer flips the "theend" flag.
	t.comment("Defining the end conditions")
	t.cmd(r, "e cmd.esil.todo=t theend=1")
	t.cmd(r, "e cmd.esil.trap=t theend=1")
	t.cmd(r, "e cmd.esil.intr=t theend=1")
	t.cmd(r, "e cmd.esil.ioer=t theend=1")

	t.comment("Initializing the end condition variable")
	t.cmd(r, "f theend=0")

	aliveCount := len(bots)
	roundsRun := 0

	// Round loop. We run up to MaxRounds *turns*; each turn advances one bot.
	for round := 0; round < spec.Config.MaxRounds; round++ {
		if aliveCount <= 1 {
			break
		}

		current := round % len(bots)
		// Skip dead bots so the round budget is spent on live ones.
		skips := 0
		for bots[current].Dead {
			current = (current + 1) % len(bots)
			skips++
			if skips >= len(bots) {
				break // everybody is dead (shouldn't happen given aliveCount check)
			}
		}
		if bots[current].Dead {
			break
		}

		roundsRun = round + 1
		bot := &bots[current]

		t.b.WriteString("[0x00000000]> ########################################################################\n")

		// Restore this bot's registers.
		t.comment("Loading the registers for bot %d (%q)", current, bot.Name)
		r.Cmd(bot.Regs)

		// Reset the end flag *before* the step. The original never reset it
		// after init, so once any bot tripped it every later turn looked like a
		// death. Resetting per-turn makes detection actually per-bot.
		r.Cmd("f theend=0")

		pc := strings.TrimSpace(mustCmd(r, "aer~$(arn PC)~[1]"))
		t.comment("ROUND %d, BOT %d (%s), PC=%s, arch=%s, bits=%s", round, current, bot.Name, pc, bot.Arch, bot.Bits)

		// Switch the disassembler/VM to this bot's arch/bits.
		t.comment("setting the architecture accordingly")
		t.cmd(r, fmt.Sprintf("e asm.arch=%s", bot.Arch))
		t.cmd(r, fmt.Sprintf("e asm.bits=%s", bot.Bits))

		// Step one instruction.
		t.comment("Stepping")
		t.cmd(r, "aes")

		// Snapshot the registers again for next time.
		t.comment("Storing the registers")
		regs := mustCmd(r, "aerR")
		bot.Regs = strings.ReplaceAll(regs, "\n", ";")

		// Show the bot's neighbourhood for the visuals.
		t.comment("Printing the arena around bot %d", current)
		t.cmd(r, fmt.Sprintf("pxc 100 @ 0x%x", bot.BaseAddr))

		// Did this bot trip an end condition?
		t.comment("Checking if bot %d died", current)
		status := t.cmd(r, "?v theend")
		if status == "0x1" {
			bot.Dead = true
			bot.DeathRound = round
			aliveCount--
			t.comment("bot %d (%q) died in round %d", current, bot.Name, round)
		}
	}

	res := &Result{
		Spec:       spec,
		Transcript: t.String(),
		Bots:       bots,
		Rounds:     roundsRun,
	}
	for _, b := range bots {
		if !b.Dead {
			res.Survivors = append(res.Survivors, b.Name)
		}
	}
	if len(res.Survivors) == 1 {
		res.Winner = res.Survivors[0]
	}
	return res, nil
}

// mustCmd runs a command and returns its trimmed output, ignoring errors (r2
// command failures are non-fatal during a run, exactly as in the original).
func mustCmd(r R2, command string) string {
	out, _ := r.Cmd(command)
	return strings.TrimSpace(out)
}
