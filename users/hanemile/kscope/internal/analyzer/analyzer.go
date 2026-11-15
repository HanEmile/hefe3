// internal/analyzer/analyzer.go — Symbol indexer, cross-reference builder,
// duplicate detector, and graph payload generator for KScope.
//
// Symbols are classified by path:
//   - include/uapi/... → KindUAPI  (stable userspace ABI)
//   - elsewhere        → KindKernel (internal, may change)
//   - EXPORT_SYMBOL*   → KindExported
//   - SYSCALL_DEFINE*  → KindSyscall
//   - name collision   → KindDuplicated
//
// Part of github.com/hanemile/kscope
package analyzer

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// SymbolKind classifies a symbol by its availability context.
type SymbolKind int

const (
	KindUnknown    SymbolKind = iota
	KindKernel                // Internal kernel symbol
	KindUAPI                  // Defined in uapi/ headers — stable ABI
	KindExported              // EXPORT_SYMBOL / EXPORT_SYMBOL_GPL
	KindSyscall               // SYSCALL_DEFINE* entry points
	KindDuplicated            // Same name in both kernel and uapi paths
)

func (k SymbolKind) String() string {
	switch k {
	case KindKernel:     return "kernel"
	case KindUAPI:       return "uapi"
	case KindExported:   return "exported"
	case KindSyscall:    return "syscall"
	case KindDuplicated: return "duplicated"
	default:             return "unknown"
	}
}

// Symbol represents a named entity (struct, typedef, function, macro, enum).
type Symbol struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	KindStr  string     `json:"kind_str"`
	File     string     `json:"file"`
	Line     int        `json:"line"`
	Column   int        `json:"column"`
	Type     string     `json:"type"` // "struct","typedef","func","macro","enum"
	Comment  string     `json:"comment,omitempty"`
	UAPIPeer string     `json:"uapi_peer,omitempty"`
}

// DuplicateReport summarises a kernel/uapi struct name collision.
type DuplicateReport struct {
	Name       string `json:"name"`
	KernelFile string `json:"kernel_file"`
	UAPIFile   string `json:"uapi_file"`
	Risk       string `json:"risk"` // "high","medium","low"
	Notes      string `json:"notes,omitempty"`
}

// XRef records one usage of a symbol inside a function body.
type XRef struct {
	Symbol   string `json:"symbol"`
	UsedIn   string `json:"used_in"`   // enclosing function name
	File     string `json:"file"`
	Line     int    `json:"line"`
	Context  string `json:"context"`   // trimmed source line
}

// ProgressFunc is called after each file is processed.
// fileDelta=1 each time; symDelta = symbols found in that file.
type ProgressFunc func(fileDelta, symDelta int64)

// Analyzer holds the complete index for one kernel source tree.
type Analyzer struct {
	root    string
	mu      sync.RWMutex
	symbols map[string][]*Symbol  // name → definitions
	byFile  map[string][]*Symbol  // relPath → symbols defined here
	xrefs   map[string][]*XRef    // symbol name → usages
	dups    []*DuplicateReport
}

// New creates an Analyzer for the given kernel root (does not index yet).
func New(root string) *Analyzer {
	return &Analyzer{
		root:    root,
		symbols: make(map[string][]*Symbol),
		byFile:  make(map[string][]*Symbol),
		xrefs:   make(map[string][]*XRef),
	}
}

// Index walks the source tree and builds the full index.
func (a *Analyzer) Index() error { return a.IndexWithProgress(nil) }

// IndexWithProgress is like Index but calls progress(1, symCount) per file.
func (a *Analyzer) IndexWithProgress(progress ProgressFunc) error {
	newSymbols := make(map[string][]*Symbol)
	newByFile  := make(map[string][]*Symbol)
	newXrefs   := make(map[string][]*XRef)

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 8)

	err := filepath.WalkDir(a.root, func(path string, d os.DirEntry, err error) error {
		if err != nil { return nil }
		base := d.Name()
		if d.IsDir() {
			switch base {
			case ".git", "Documentation", "tools", "scripts", "samples":
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(base) { return nil }

		rel, _ := filepath.Rel(a.root, path)
		wg.Add(1)
		sem <- struct{}{}
		go func(full, rel string) {
			defer wg.Done()
			defer func() { <-sem }()

			syms, refs := parseFile(full, rel)
			symCount := int64(len(syms))
			mu.Lock()
			for _, s := range syms {
				newSymbols[s.Name] = append(newSymbols[s.Name], s)
				newByFile[rel]      = append(newByFile[rel], s)
			}
			for _, x := range refs {
				newXrefs[x.Symbol] = append(newXrefs[x.Symbol], x)
			}
			mu.Unlock()
			if progress != nil { progress(1, symCount) }
		}(path, rel)
		return nil
	})

	wg.Wait()
	if err != nil { return err }

	dups := detectDuplicates(newSymbols)

	a.mu.Lock()
	a.symbols = newSymbols
	a.byFile  = newByFile
	a.xrefs   = newXrefs
	a.dups    = dups
	a.mu.Unlock()
	return nil
}

// ── Query API ─────────────────────────────────────────────────────────────────

func (a *Analyzer) Lookup(name string) []*Symbol {
	a.mu.RLock(); defer a.mu.RUnlock()
	return a.symbols[name]
}

func (a *Analyzer) FileSymbols(relPath string) []*Symbol {
	a.mu.RLock(); defer a.mu.RUnlock()
	return a.byFile[relPath]
}

func (a *Analyzer) Duplicates() []*DuplicateReport {
	a.mu.RLock(); defer a.mu.RUnlock()
	return a.dups
}

// XRefs returns all recorded usages of the named symbol.
func (a *Analyzer) XRefs(name string) []*XRef {
	a.mu.RLock(); defer a.mu.RUnlock()
	return a.xrefs[name]
}

// XRefGraphData builds a symbol-usage graph centred on one symbol (or all).
func (a *Analyzer) XRefGraphData(center string, depth int) GraphPayload {
	a.mu.RLock(); defer a.mu.RUnlock()

	nodes := []GraphNode{}
	edges := []GraphEdge{}
	idx   := map[string]int{}

	add := func(id, label, kind string) int {
		if i, ok := idx[id]; ok { return i }
		i := len(nodes)
		nodes = append(nodes, GraphNode{ID: id, Label: label, Kind: kind})
		idx[id] = i
		return i
	}

	var visit func(name string, d int)
	visit = func(name string, d int) {
		if d < 0 { return }
		defs := a.symbols[name]
		if len(defs) == 0 { return }
		symIdx := add("sym:"+name, name, defs[0].KindStr)
		for _, x := range a.xrefs[name] {
			fnIdx := add("fn:"+x.UsedIn+":"+x.File, x.UsedIn, "function")
			fIdx  := add("file:"+x.File, x.File, "file")
			edges = append(edges, GraphEdge{Source: fnIdx, Target: symIdx, Weight: 1, Label: "uses"})
			edges = append(edges, GraphEdge{Source: fIdx,  Target: fnIdx,  Weight: 1, Label: "contains"})
			if d > 0 { visit(x.UsedIn, d-1) }
		}
	}

	if center != "" {
		visit(center, depth)
	} else {
		// Top-N most-used symbols
		type ranked struct{ name string; n int }
		var top []ranked
		for name, refs := range a.xrefs {
			top = append(top, ranked{name, len(refs)})
		}
		// simple partial sort
		for i := 0; i < len(top) && i < 80; i++ {
			for j := i + 1; j < len(top); j++ {
				if top[j].n > top[i].n { top[i], top[j] = top[j], top[i] }
			}
		}
		lim := 80
		if len(top) < lim { lim = len(top) }
		for _, r := range top[:lim] {
			visit(r.name, 0)
		}
	}
	return GraphPayload{Nodes: nodes, Edges: edges}
}

func (a *Analyzer) Search(query string, limit int) []*Symbol {
	a.mu.RLock(); defer a.mu.RUnlock()
	q := strings.ToLower(query)
	var out []*Symbol
	seen := map[string]bool{}
	for name, syms := range a.symbols {
		if strings.Contains(strings.ToLower(name), q) {
			for _, s := range syms {
				key := s.File + ":" + s.Name
				if !seen[key] { out = append(out, s); seen[key] = true }
			}
		}
		if len(out) >= limit { break }
	}
	return out
}

// GraphData returns the full file/symbol relationship graph.
func (a *Analyzer) GraphData(maxNodes int) GraphPayload {
	a.mu.RLock(); defer a.mu.RUnlock()

	nodes   := []GraphNode{}
	edges   := []GraphEdge{}
	nodeIdx := map[string]int{}

	add := func(id, label, kind string) int {
		if i, ok := nodeIdx[id]; ok { return i }
		i := len(nodes)
		nodes = append(nodes, GraphNode{ID: id, Label: label, Kind: kind})
		nodeIdx[id] = i
		return i
	}

	count := 0
	for name, syms := range a.symbols {
		if count >= maxNodes { break }
		for _, s := range syms {
			add("file:"+s.File, s.File, "file")
			add("dir:"+filepath.Dir(s.File), filepath.Dir(s.File), "dir")
			nI := add("sym:"+name+":"+s.KindStr, name, s.KindStr)
			fI := nodeIdx["file:"+s.File]
			edges = append(edges, GraphEdge{Source: fI, Target: nI, Weight: 1})
		}
		count++
	}
	for _, d := range a.dups {
		kI, ok1 := nodeIdx["file:"+d.KernelFile]
		uI, ok2 := nodeIdx["file:"+d.UAPIFile]
		if ok1 && ok2 {
			edges = append(edges, GraphEdge{Source: kI, Target: uI, Weight: 3, Label: "duplicate:" + d.Name})
		}
	}
	return GraphPayload{Nodes: nodes, Edges: edges}
}

// ── internal parsers ──────────────────────────────────────────────────────────

func isSourceFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".c" || ext == ".h"
}

var (
	reStruct  = regexp.MustCompile(`(?m)^(?:typedef\s+)?struct\s+(\w+)`)
	reEnum    = regexp.MustCompile(`(?m)^(?:typedef\s+)?enum\s+(\w+)`)
	reMacro   = regexp.MustCompile(`(?m)^#define\s+(\w+)`)
	reExport  = regexp.MustCompile(`EXPORT_SYMBOL(?:_GPL)?\((\w+)\)`)
	reSyscall = regexp.MustCompile(`(?:SYSCALL_DEFINE\d+|__x64_sys_)(\w+)`)
	// function definition: return-type fname(
	reFuncDef = regexp.MustCompile(`(?m)^(?:static\s+)?(?:inline\s+)?(?:\w[\w\s\*]+)\s+(\w+)\s*\([^;)]*\)\s*\{`)
	// identifier used in body
	reIdent   = regexp.MustCompile(`\b([A-Za-z_]\w{2,})\b`)
)

func classifyPath(rel string) SymbolKind {
	norm := filepath.ToSlash(rel)
	if strings.Contains(norm, "/uapi/") || strings.HasPrefix(norm, "uapi/") {
		return KindUAPI
	}
	return KindKernel
}

// parseFile extracts symbol definitions AND cross-reference usages.
func parseFile(fullPath, relPath string) ([]*Symbol, []*XRef) {
	f, err := os.Open(fullPath)
	if err != nil { return nil, nil }
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var lines []string
	for scanner.Scan() { lines = append(lines, scanner.Text()) }
	content := strings.Join(lines, "\n")

	baseKind := classifyPath(relPath)
	var syms []*Symbol
	seen := map[string]bool{}

	addSym := func(name, typ string, line int, kind SymbolKind) {
		key := name + ":" + typ
		if seen[key] { return }
		seen[key] = true
		syms = append(syms, &Symbol{
			Name: name, Kind: kind, KindStr: kind.String(),
			File: relPath, Line: line, Type: typ,
		})
	}

	lineOf := func(pos int) int { return strings.Count(content[:pos], "\n") + 1 }

	for _, m := range reStruct.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "struct", lineOf(m[0]), baseKind)
	}
	for _, m := range reEnum.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "enum", lineOf(m[0]), baseKind)
	}
	for _, m := range reMacro.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "macro", lineOf(m[0]), baseKind)
	}
	for _, m := range reExport.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "func", lineOf(m[0]), KindExported)
	}
	for _, m := range reSyscall.FindAllStringSubmatchIndex(content, -1) {
		addSym(content[m[2]:m[3]], "func", lineOf(m[0]), KindSyscall)
	}

	// Build xrefs: walk function bodies and record identifier usages.
	// We only do this for .c files to keep noise down.
	var xrefs []*XRef
	if strings.HasSuffix(relPath, ".c") {
		// Find function boundaries
		funcs := reFuncDef.FindAllStringSubmatchIndex(content, -1)
		for fi, fm := range funcs {
			fnName := content[fm[2]:fm[3]]
			bodyStart := fm[1]
			bodyEnd := len(content)
			if fi+1 < len(funcs) { bodyEnd = funcs[fi+1][0] }
			body := content[bodyStart:bodyEnd]

			for _, im := range reIdent.FindAllStringSubmatchIndex(body, -1) {
				ident := body[im[2]:im[3]]
				if ident == fnName { continue }
				// Only track names that look like kernel symbols (contain _ or uppercase)
				if !strings.Contains(ident, "_") && ident == strings.ToLower(ident) { continue }
				ln := lineOf(bodyStart+im[0])
				ctx := ""
				if ln-1 < len(lines) { ctx = strings.TrimSpace(lines[ln-1]) }
				xrefs = append(xrefs, &XRef{
					Symbol: ident, UsedIn: fnName,
					File: relPath, Line: ln, Context: ctx,
				})
			}
		}
	}

	return syms, xrefs
}

func detectDuplicates(symbols map[string][]*Symbol) []*DuplicateReport {
	var dups []*DuplicateReport
	for name, syms := range symbols {
		var kernel, uapi *Symbol
		for _, s := range syms {
			if s.Type != "struct" && s.Type != "enum" && s.Type != "typedef" { continue }
			if s.Kind == KindUAPI && uapi == nil   { uapi = s }
			if s.Kind == KindKernel && kernel == nil { kernel = s }
		}
		if kernel != nil && uapi != nil {
			risk := "medium"
			if strings.HasSuffix(kernel.File, ".h") { risk = "high" }
			dups = append(dups, &DuplicateReport{
				Name: name, KernelFile: kernel.File, UAPIFile: uapi.File,
				Risk: risk, Notes: "'" + name + "' defined in both kernel and uapi paths.",
			})
			kernel.Kind = KindDuplicated; kernel.KindStr = KindDuplicated.String(); kernel.UAPIPeer = uapi.File
			uapi.Kind   = KindDuplicated; uapi.KindStr   = KindDuplicated.String(); uapi.UAPIPeer   = kernel.File
		}
	}
	return dups
}

// ── Graph types ───────────────────────────────────────────────────────────────

type GraphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

type GraphEdge struct {
	Source int    `json:"source"`
	Target int    `json:"target"`
	Weight int    `json:"weight"`
	Label  string `json:"label,omitempty"`
}

type GraphPayload struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}
