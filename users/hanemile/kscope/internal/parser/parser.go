// internal/parser/parser.go — Syntax highlighter for C/H source files.
// Renders annotated HTML tables. Correctly handles multiline block comments
// by scanning the full file content before splitting into per-line output.
//
// Part of github.com/hanemile/kscope
package parser

import (
	"bufio"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
)

// CSS class names for syntax token kinds.
const (
	TokKeyword = "kw"
	TokType    = "ty"
	TokFunc    = "fn"
	TokMacro   = "mc"
	TokString  = "st"
	TokComment = "cm"
	TokNumber  = "nb"
)

// HighlightResult is the rendered HTML for one source file.
type HighlightResult struct {
	HTML        string           `json:"html"`
	LineSymbols map[int][]string `json:"line_symbols"`
	TotalLines  int              `json:"total_lines"`
}

// Annotation marks a source line with a symbol kind for the sidebar.
type Annotation struct {
	Line int
	Kind string // kernel | uapi | duplicated | exported | syscall
	Name string
}

// Highlight reads a source file and returns annotated HTML.
func Highlight(path string, annotations map[int][]Annotation) (*HighlightResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	lineSyms := make(map[int][]string)
	if annotations != nil {
		for ln, anns := range annotations {
			for _, a := range anns {
				lineSyms[ln] = append(lineSyms[ln], a.Kind)
			}
		}
	}
	return &HighlightResult{
		HTML:        renderHTML(lines, annotations),
		LineSymbols: lineSyms,
		TotalLines:  len(lines),
	}, nil
}

// ── Regexes ───────────────────────────────────────────────────────────────────

var (
	reKW = regexp.MustCompile(
		`\b(auto|break|case|char|const|continue|default|do|double|else|enum|` +
			`extern|float|for|goto|if|inline|int|long|register|restrict|return|` +
			`short|signed|sizeof|static|struct|switch|typedef|union|unsigned|void|` +
			`volatile|while|_Alignas|_Alignof|_Atomic|_Bool|_Complex|_Generic|` +
			`_Imaginary|_Noreturn|_Static_assert|_Thread_local|` +
			`__attribute__|__extension__|__typeof__|NULL|true|false|nullptr)\b`)
	reType  = regexp.MustCompile(`\b(u8|u16|u32|u64|s8|s16|s32|s64|__u8|__u16|__u32|__u64|__le16|__le32|__le64|__be16|__be32|__be64|__kernel_\w+|size_t|ssize_t|off_t|pid_t|uid_t|gid_t|dev_t|ino_t|mode_t|nlink_t|loff_t|pgoff_t|sector_t|bool|atomic_t|spinlock_t|mutex|rcu_head|list_head|hlist_node|hlist_head|rb_node|rb_root|wait_queue_head_t)\b`)
	reMacro = regexp.MustCompile(`(?m)^(#\s*\w+)`)
	reStr   = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	reNum   = regexp.MustCompile(`\b(0x[0-9a-fA-F]+|[0-9]+[uUlLfF]*)\b`)
	reLineC = regexp.MustCompile(`//[^\n]*`)
	// (?s) makes . match newlines — correctly captures multiline block comments
	reBlkC = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reFn   = regexp.MustCompile(`\b([a-z_][a-z0-9_]*)\s*\(`)
)

// ── Per-line annotation helpers ───────────────────────────────────────────────

func annClass(anns []Annotation) (cls, title string) {
	if len(anns) == 0 {
		return "", ""
	}
	kinds := map[string]bool{}
	var names []string
	for _, a := range anns {
		kinds[a.Kind] = true
		names = append(names, a.Name+" ("+a.Kind+")")
	}
	switch {
	case kinds["duplicated"]: cls = "ann-d"
	case kinds["uapi"]:       cls = "ann-u"
	case kinds["exported"]:   cls = "ann-e"
	case kinds["syscall"]:    cls = "ann-s"
	case kinds["kernel"]:     cls = "ann-k"
	}
	return cls, strings.Join(names, ", ")
}

func annPipColor(kind string) string {
	return map[string]string{
		"kernel":     "#00ff7f",
		"uapi":       "#ffe000",
		"duplicated": "#ff2d2d",
		"exported":   "#cc44ff",
		"syscall":    "#00e5ff",
	}[kind]
}

// ── HTML renderer — multiline-comment-aware ───────────────────────────────────

// blockCommentRanges finds all /*…*/ spans in the full (joined) content.
type byteRange struct{ s, e int }

func blockCommentRanges(content string) []byteRange {
	var ranges []byteRange
	for _, m := range reBlkC.FindAllStringIndex(content, -1) {
		ranges = append(ranges, byteRange{m[0], m[1]})
	}
	return ranges
}

func posInComment(pos int, ranges []byteRange) bool {
	for _, r := range ranges {
		if pos >= r.s && pos < r.e {
			return true
		}
	}
	return false
}

func renderHTML(lines []string, annotations map[int][]Annotation) string {
	// Build full content and locate block comment ranges once.
	content := strings.Join(lines, "\n")
	blkRanges := blockCommentRanges(content)

	// Build cumulative byte offsets per line so we can map line→byte position.
	lineOffset := make([]int, len(lines)+1)
	off := 0
	for i, l := range lines {
		lineOffset[i] = off
		off += len(l) + 1 // +1 for the \n we joined with
	}
	lineOffset[len(lines)] = off

	var sb strings.Builder
	sb.WriteString(`<table class="src-table"><tbody>`)

	for i, raw := range lines {
		ln     := i + 1
		lStart := lineOffset[i]
		lEnd   := lineOffset[i] + len(raw)

		var anns []Annotation
		if annotations != nil {
			anns = annotations[ln]
		}
		cls, title := annClass(anns)
		rowCls := "src-line"
		if cls != "" {
			rowCls += " " + cls
		}

		sb.WriteString(`<tr class="` + rowCls + `">`)
		sb.WriteString(fmt.Sprintf(
			`<td class="src-ln" id="L%d" title="%s">%d</td>`,
			ln, html.EscapeString(title), ln))

		// Annotation pip column
		sb.WriteString(`<td class="src-ann">`)
		for _, a := range anns {
			if c := annPipColor(a.Kind); c != "" {
				sb.WriteString(`<div class="ann-pip" style="background:` + c + `"></div>`)
			}
		}
		sb.WriteString(`</td>`)

		// Code column — handle block-comment state
		sb.WriteString(`<td class="src-cd">`)

		if len(raw) == 0 {
			sb.WriteString(`</td></tr>` + "\n")
			continue
		}

		startInComment := posInComment(lStart, blkRanges)
		endInComment   := lEnd > lStart && posInComment(lEnd-1, blkRanges)

		if startInComment && endInComment {
			// Entire line is inside a block comment
			sb.WriteString(`<span class="hl-cm">` + html.EscapeString(raw) + `</span>`)
		} else if startInComment {
			// Line starts inside a comment; find where it ends
			commentEnd := 0
			for _, r := range blkRanges {
				if lStart >= r.s && r.e > lStart {
					commentEnd = r.e - lStart
					break
				}
			}
			if commentEnd > len(raw) {
				commentEnd = len(raw)
			}
			sb.WriteString(`<span class="hl-cm">` + html.EscapeString(raw[:commentEnd]) + `</span>`)
			if commentEnd < len(raw) {
				sb.WriteString(highlightLine(raw[commentEnd:]))
			}
		} else if endInComment {
			// Line ends inside a comment; find where comment starts on this line
			commentStart := len(raw)
			for _, r := range blkRanges {
				if r.s >= lStart && r.s < lEnd {
					cs := r.s - lStart
					if cs < commentStart {
						commentStart = cs
					}
				}
			}
			sb.WriteString(highlightLine(raw[:commentStart]))
			sb.WriteString(`<span class="hl-cm">` + html.EscapeString(raw[commentStart:]) + `</span>`)
		} else {
			sb.WriteString(highlightLine(raw))
		}

		sb.WriteString(`</td></tr>` + "\n")
	}

	sb.WriteString("</tbody></table>")
	return sb.String()
}

// ── Per-line token highlighter ────────────────────────────────────────────────

type tok struct {
	s, e int
	cls  string
}

// highlightLine tokenises a single source line that is NOT inside a block comment.
// Block comments that START on this line are handled by adding them first.
func highlightLine(line string) string {
	esc := html.EscapeString(line)
	var toks []tok

	tryAdd := func(s, e int, cls string) {
		for _, t := range toks {
			if s < t.e && e > t.s {
				return // overlaps existing token
			}
		}
		toks = append(toks, tok{s, e, cls})
	}

	// Priority order: block comments > line comments > strings > macros >
	// keywords > types > numbers > function calls
	for _, m := range reBlkC.FindAllStringIndex(esc, -1) {
		toks = append(toks, tok{m[0], m[1], TokComment})
	}
	for _, m := range reLineC.FindAllStringIndex(esc, -1) {
		tryAdd(m[0], m[1], TokComment)
	}
	for _, m := range reStr.FindAllStringIndex(esc, -1)   { tryAdd(m[0], m[1], TokString) }
	for _, m := range reMacro.FindAllStringIndex(esc, -1) { tryAdd(m[0], m[1], TokMacro) }
	for _, m := range reKW.FindAllStringIndex(esc, -1)    { tryAdd(m[0], m[1], TokKeyword) }
	for _, m := range reType.FindAllStringIndex(esc, -1)  { tryAdd(m[0], m[1], TokType) }
	for _, m := range reNum.FindAllStringIndex(esc, -1)   { tryAdd(m[0], m[1], TokNumber) }
	for _, m := range reFn.FindAllStringIndex(esc, -1)    { tryAdd(m[0], m[1], TokFunc) }

	if len(toks) == 0 {
		return esc
	}
	// Insertion sort (small N)
	for i := 1; i < len(toks); i++ {
		for j := i; j > 0 && toks[j].s < toks[j-1].s; j-- {
			toks[j], toks[j-1] = toks[j-1], toks[j]
		}
	}

	var out strings.Builder
	pos := 0
	for _, t := range toks {
		if t.s < pos {
			continue
		}
		out.WriteString(esc[pos:t.s])
		out.WriteString(`<span class="hl-` + t.cls + `">`)
		out.WriteString(esc[t.s:t.e])
		out.WriteString(`</span>`)
		pos = t.e
	}
	out.WriteString(esc[pos:])
	return out.String()
}
