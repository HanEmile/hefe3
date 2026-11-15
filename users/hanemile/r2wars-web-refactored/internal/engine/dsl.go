package engine

// DSL: a minimal, line-based text format for declaring battles headlessly, for
// tests, a CLI and reproducible battles. Grammar:
//
//	# comment            -- whole-line comment (ignored); blank lines ignored
//	battle "name"        -- battle name (optional)
//	arena 4096           -- arena size in bytes
//	rounds 200           -- max rounds (turns)
//	gap 64               -- placement gap in bytes (optional)
//
//	# inline-source bot: an opening "{" ends the header line; every following
//	# line up to a line containing only "}" is the verbatim assembly source.
//	bot "warrior1" arch x86 bits 32 {
//	    mov eax, 1
//	    nop
//	    jmp 0
//	}
//
//	# file-source bot: source is read from the referenced path.
//	bot "warrior2" arch x86 bits 32 source "/path/to/file.asm"
//
// Comments are not stripped inside an inline { ... } block, so assembly that
// happens to contain '#' survives. Round-trip: ParseDSL(FormatDSL(s)) recovers
// an inline-source spec exactly (file-source bots are inlined on format).

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"git.emile.space/r2wars-web/internal/model"
)

// ParseDSL parses a battle declaration from r.
func ParseDSL(r io.Reader) (Spec, error) {
	var spec Spec
	spec.Config.PlacementGap = 0 // 0 => engine default

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0

	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		keyword, rest := splitFirst(line)
		switch keyword {
		case "battle":
			name, err := unquote(strings.TrimSpace(rest))
			if err != nil {
				return Spec{}, fmt.Errorf("line %d: battle name: %w", lineNo, err)
			}
			spec.Name = name

		case "arena":
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return Spec{}, fmt.Errorf("line %d: arena size: %w", lineNo, err)
			}
			spec.Config.ArenaSize = n

		case "rounds":
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return Spec{}, fmt.Errorf("line %d: rounds: %w", lineNo, err)
			}
			spec.Config.MaxRounds = n

		case "gap":
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return Spec{}, fmt.Errorf("line %d: gap: %w", lineNo, err)
			}
			spec.Config.PlacementGap = n

		case "bot":
			bot, inline, err := parseBotHeader(rest, lineNo)
			if err != nil {
				return Spec{}, err
			}
			if inline {
				src, err := readInlineBlock(sc, &lineNo)
				if err != nil {
					return Spec{}, err
				}
				bot.Source = src
			}
			spec.Bots = append(spec.Bots, bot)

		default:
			return Spec{}, fmt.Errorf("line %d: unknown directive %q", lineNo, keyword)
		}
	}
	if err := sc.Err(); err != nil {
		return Spec{}, fmt.Errorf("reading dsl: %w", err)
	}
	return spec, nil
}

// parseBotHeader parses everything after the leading "bot " keyword. It returns
// the bot (without source unless a "source" file was named) and whether an
// inline "{" block follows.
func parseBotHeader(rest string, lineNo int) (BotSpec, bool, error) {
	var bot BotSpec

	rest = strings.TrimSpace(rest)
	name, after, err := takeQuoted(rest)
	if err != nil {
		return bot, false, fmt.Errorf("line %d: bot name: %w", lineNo, err)
	}
	bot.Name = name

	// Detect a trailing inline block opener.
	inline := false
	after = strings.TrimSpace(after)
	if strings.HasSuffix(after, "{") {
		inline = true
		after = strings.TrimSpace(strings.TrimSuffix(after, "{"))
	}

	// Parse the "arch X bits Y [source \"path\"]" attribute list.
	fields := strings.Fields(after)
	i := 0
	for i < len(fields) {
		switch fields[i] {
		case "arch":
			if i+1 >= len(fields) {
				return bot, false, fmt.Errorf("line %d: arch missing value", lineNo)
			}
			bot.Arch = fields[i+1]
			i += 2
		case "bits":
			if i+1 >= len(fields) {
				return bot, false, fmt.Errorf("line %d: bits missing value", lineNo)
			}
			bot.Bits = fields[i+1]
			i += 2
		case "source":
			// The remainder of the line is a quoted path.
			rem := strings.TrimSpace(strings.Join(fields[i+1:], " "))
			path, err := unquote(rem)
			if err != nil {
				return bot, false, fmt.Errorf("line %d: source path: %w", lineNo, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return bot, false, fmt.Errorf("line %d: reading source %q: %w", lineNo, path, err)
			}
			bot.Source = string(data)
			i = len(fields)
		default:
			return bot, false, fmt.Errorf("line %d: unexpected token %q in bot header", lineNo, fields[i])
		}
	}

	if bot.Arch == "" {
		return bot, false, fmt.Errorf("line %d: bot %q missing arch", lineNo, bot.Name)
	}
	if bot.Bits == "" {
		return bot, false, fmt.Errorf("line %d: bot %q missing bits", lineNo, bot.Name)
	}
	if !inline && bot.Source == "" {
		return bot, false, fmt.Errorf("line %d: bot %q has neither inline source nor source file", lineNo, bot.Name)
	}
	return bot, inline, nil
}

// readInlineBlock consumes lines until a line that is exactly "}". The common
// leading indentation shared by all non-blank lines is stripped, so source
// indented for readability (as FormatDSL emits) round-trips to its original
// unindented form.
func readInlineBlock(sc *bufio.Scanner, lineNo *int) (string, error) {
	var lines []string
	closed := false
	for sc.Scan() {
		*lineNo++
		line := sc.Text()
		if strings.TrimSpace(line) == "}" {
			closed = true
			break
		}
		lines = append(lines, line)
	}
	if !closed {
		return "", fmt.Errorf("line %d: unterminated inline source block (missing closing '}')", *lineNo)
	}
	return strings.Join(dedent(lines), "\n"), nil
}

// dedent removes the longest leading-whitespace prefix common to every non-blank
// line.
func dedent(lines []string) []string {
	prefix := ""
	first := true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			prefix = indent
			first = false
			continue
		}
		prefix = commonPrefix(prefix, indent)
	}
	if prefix == "" {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimPrefix(l, prefix)
	}
	return out
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// FormatDSL renders a Spec back to DSL text. All bots are emitted with inline
// source blocks so the output is self-contained and round-trips.
func FormatDSL(s Spec) string {
	var b strings.Builder
	if s.Name != "" {
		fmt.Fprintf(&b, "battle %q\n", s.Name)
	}
	fmt.Fprintf(&b, "arena %d\n", s.Config.ArenaSize)
	fmt.Fprintf(&b, "rounds %d\n", s.Config.MaxRounds)
	if s.Config.PlacementGap > 0 {
		fmt.Fprintf(&b, "gap %d\n", s.Config.PlacementGap)
	}
	for _, bot := range s.Bots {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "bot %q arch %s bits %s {\n", bot.Name, bot.Arch, bot.Bits)
		src := strings.ReplaceAll(bot.Source, "\r\n", "\n")
		for _, line := range strings.Split(src, "\n") {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// SpecFromModel builds a Spec from a fully-populated model.Battle. The web
// layer deep-fetches bots (their Source plus their Archs/Bits), so everything
// the engine needs is already in memory: this avoids a callback into the store.
// As the original did, each bot uses its first arch and first bit; an error is
// returned if a bot has none.
func SpecFromModel(b model.Battle) (Spec, error) {
	spec := Spec{
		Name: b.Name,
		Config: Config{
			ArenaSize: b.ArenaSize,
			MaxRounds: b.MaxRounds,
		},
	}
	for _, bot := range b.Bots {
		if len(bot.Archs) == 0 {
			return Spec{}, fmt.Errorf("bot %q (id %d) has no arch", bot.Name, bot.ID)
		}
		if len(bot.Bits) == 0 {
			return Spec{}, fmt.Errorf("bot %q (id %d) has no bits", bot.Name, bot.ID)
		}
		spec.Bots = append(spec.Bots, BotSpec{
			Name:   bot.Name,
			Arch:   bot.Archs[0].Name,
			Bits:   bot.Bits[0].Name,
			Source: bot.Source,
		})
	}
	return spec, nil
}

// --- small parsing helpers ---

// splitFirst splits a line into its first whitespace-delimited token and the
// remainder.
func splitFirst(line string) (first, rest string) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return line, ""
	}
	return line[:idx], strings.TrimSpace(line[idx+1:])
}

// unquote removes surrounding double quotes from s, erroring if absent.
func unquote(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("expected a double-quoted string, got %q", s)
	}
	return s[1 : len(s)-1], nil
}

// takeQuoted reads a leading "..."-quoted token from s and returns it along
// with the rest of the string after the closing quote.
func takeQuoted(s string) (value, rest string, err error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '"' {
		return "", "", fmt.Errorf("expected a double-quoted string, got %q", s)
	}
	end := strings.IndexByte(s[1:], '"')
	if end < 0 {
		return "", "", fmt.Errorf("unterminated quoted string in %q", s)
	}
	return s[1 : 1+end], s[2+end:], nil
}
