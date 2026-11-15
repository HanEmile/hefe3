// internal/store/index.go — Thread-safe in-memory index store.
// Index is an immutable snapshot swapped atomically on re-index.
// VersionStore wraps one Index plus a live AnnotationStore.
//
// Part of github.com/hanemile/kscope
package store

import (
	"path/filepath"
	"strings"
	"sync"
)

// ── Symbol kinds ──────────────────────────────────────────────────────────────

type SymbolKind int

const (
	KindUnknown    SymbolKind = iota
	KindKernel
	KindUAPI
	KindExported
	KindSyscall
	KindDuplicated
	KindFunction
)

func (k SymbolKind) String() string {
	names := [...]string{"unknown","kernel","uapi","exported","syscall","duplicated","function"}
	if int(k) < len(names) { return names[k] }
	return "unknown"
}

func ClassifyPath(relPath string) SymbolKind {
	norm := filepath.ToSlash(relPath)
	if strings.Contains(norm, "/uapi/") || strings.HasPrefix(norm, "uapi/") {
		return KindUAPI
	}
	return KindKernel
}

// ── Data types ────────────────────────────────────────────────────────────────

type Symbol struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	KindStr  string     `json:"kind_str"`
	File     string     `json:"file"`
	Line     int        `json:"line"`
	Type     string     `json:"type"` // struct|enum|macro|func|typedef
	UAPIPeer string     `json:"uapi_peer,omitempty"`
}

type DuplicateReport struct {
	Name       string `json:"name"`
	KernelFile string `json:"kernel_file"`
	UAPIFile   string `json:"uapi_file"`
	Risk       string `json:"risk"`
	Notes      string `json:"notes,omitempty"`
}

type XRef struct {
	Symbol  string `json:"symbol"`
	UsedIn  string `json:"used_in"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Context string `json:"context"`
}

type CallEdge struct {
	Caller string `json:"caller"`
	Callee string `json:"callee"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

type Constant struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Kind    string `json:"kind"` // int|string|expr|flag
	KindStr string `json:"kind_str"`
}

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

// ── Index ─────────────────────────────────────────────────────────────────────

type Index struct {
	Symbols   map[string][]*Symbol
	ByFile    map[string][]*Symbol
	XRefs     map[string][]*XRef
	CallEdges map[string][]*CallEdge // caller → []CallEdge
	Constants map[string]*Constant
	Dups      []*DuplicateReport
}

func NewIndex() *Index {
	return &Index{
		Symbols:   make(map[string][]*Symbol),
		ByFile:    make(map[string][]*Symbol),
		XRefs:     make(map[string][]*XRef),
		CallEdges: make(map[string][]*CallEdge),
		Constants: make(map[string]*Constant),
	}
}

// ── VersionStore ──────────────────────────────────────────────────────────────

type VersionStore struct {
	Label       string
	Root        string
	GitTag      string
	Indexed     bool
	Annotations *AnnotationStore
	mu          sync.RWMutex
	idx         *Index
}

func NewVersionStore(label, root, gitTag string) *VersionStore {
	return &VersionStore{
		Label:       label,
		Root:        root,
		GitTag:      gitTag,
		idx:         NewIndex(),
		Annotations: NewAnnotationStore(),
	}
}

func (vs *VersionStore) Swap(idx *Index) {
	vs.mu.Lock(); vs.idx = idx; vs.Indexed = true; vs.mu.Unlock()
}

func (vs *VersionStore) Read() *Index {
	vs.mu.RLock(); defer vs.mu.RUnlock(); return vs.idx
}

// ── Query helpers ─────────────────────────────────────────────────────────────

func (idx *Index) Lookup(name string) []*Symbol     { return idx.Symbols[name] }
func (idx *Index) FileSymbols(p string) []*Symbol   { return idx.ByFile[p] }
func (idx *Index) XRefsFor(name string) []*XRef     { return idx.XRefs[name] }
func (idx *Index) CallsBy(fn string) []*CallEdge    { return idx.CallEdges[fn] }
func (idx *Index) ConstantByName(n string) *Constant { return idx.Constants[n] }
func (idx *Index) SymbolCount() int                  { return len(idx.Symbols) }
func (idx *Index) FileCount()   int                  { return len(idx.ByFile) }

func (idx *Index) XRefCount() int {
	n := 0; for _, r := range idx.XRefs { n += len(r) }; return n
}
func (idx *Index) CallEdgeCount() int {
	n := 0; for _, e := range idx.CallEdges { n += len(e) }; return n
}

func (idx *Index) Search(q string, limit int) []*Symbol {
	q = strings.ToLower(q)
	var out []*Symbol
	seen := map[string]bool{}
	for name, syms := range idx.Symbols {
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

// ── Graph builders ────────────────────────────────────────────────────────────

func (idx *Index) GraphData(maxNodes int) GraphPayload {
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
	for name, syms := range idx.Symbols {
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
	for _, d := range idx.Dups {
		kI, ok1 := nodeIdx["file:"+d.KernelFile]
		uI, ok2 := nodeIdx["file:"+d.UAPIFile]
		if ok1 && ok2 {
			edges = append(edges, GraphEdge{Source: kI, Target: uI, Weight: 3, Label: "duplicate:" + d.Name})
		}
	}
	return GraphPayload{Nodes: nodes, Edges: edges}
}

// XRefGraphData builds a focused usage graph for a single symbol.
//
// For a centered query (center != ""):
//   - The center symbol is one node (coloured by its kind).
//   - Each *unique function* that uses it becomes one node.
//   - File nodes are omitted to reduce noise; the file path is encoded in the
//     function node's ID so hover tooltips can show it.
//   - Disconnected nodes (functions that have no path back to center) are
//     never added because every function node connects to the center.
//   - The number of caller nodes is capped at maxCallers to keep the graph
//     readable; callers are sorted by usage count (most frequent first).
//
// For the overview (center == ""), the top-N most-referenced symbols are shown.
func (idx *Index) XRefGraphData(center string, depth int) GraphPayload {
	const maxCallers = 40 // cap per symbol to keep the graph readable

	nodes  := []GraphNode{}
	edges  := []GraphEdge{}
	idxMap := map[string]int{}

	add := func(id, label, kind string) int {
		if i, ok := idxMap[id]; ok { return i }
		i := len(nodes)
		nodes = append(nodes, GraphNode{ID: id, Label: label, Kind: kind})
		idxMap[id] = i
		return i
	}

	if center != "" {
		// Center node
		defs := idx.Symbols[center]
		centerKind := "unknown"
		if len(defs) > 0 { centerKind = defs[0].KindStr }
		centerI := add("sym:"+center, center, centerKind)

		// Collect xrefs, deduplicate by (function, file) and sort by count
		type caller struct {
			Fn   string
			File string
			Cnt  int
		}
		seen := map[string]*caller{}
		for _, x := range idx.XRefs[center] {
			key := x.UsedIn + "|" + x.File
			if c, ok := seen[key]; ok {
				c.Cnt++
			} else {
				seen[key] = &caller{Fn: x.UsedIn, File: x.File, Cnt: 1}
			}
		}
		// Sort: most frequent callers first
		callers := make([]*caller, 0, len(seen))
		for _, c := range seen { callers = append(callers, c) }
		for i := 1; i < len(callers); i++ {
			for j := i; j > 0 && callers[j].Cnt > callers[j-1].Cnt; j-- {
				callers[j], callers[j-1] = callers[j-1], callers[j]
			}
		}
		if len(callers) > maxCallers { callers = callers[:maxCallers] }

		for _, c := range callers {
			// Encode short file path into label for readability
			shortFile := c.File
			if idx := strings.LastIndex(c.File, "/"); idx >= 0 {
				shortFile = c.File[idx+1:]
			}
			label := c.Fn
			if shortFile != "" { label = c.Fn + "  (" + shortFile + ")" }
			fnI := add("fn:"+c.Fn+":"+c.File, label, "function")
			// Edge: function → center symbol
			edges = append(edges, GraphEdge{
				Source: fnI, Target: centerI,
				Weight: c.Cnt, Label: "uses",
			})
			// If depth>0 also recurse to show what calls those callers
			if depth > 0 {
				callerDefs := idx.Symbols[c.Fn]
				callerKind := "function"
				if len(callerDefs) > 0 { callerKind = callerDefs[0].KindStr }
				for _, x2 := range idx.XRefs[c.Fn] {
					if len(nodes) > maxCallers*3 { break } // hard cap on total nodes
					grandI := add("fn:"+x2.UsedIn+":"+x2.File, x2.UsedIn, callerKind)
					edges = append(edges, GraphEdge{Source: grandI, Target: fnI, Weight: 1, Label: "uses"})
				}
			}
		}
	} else {
		// Overview: top-N most-referenced symbols, no file nodes
		type ranked struct{ n string; c int }
		var top []ranked
		for nm, refs := range idx.XRefs { top = append(top, ranked{nm, len(refs)}) }
		for i := 0; i < len(top) && i < 60; i++ {
			for j := i + 1; j < len(top); j++ { if top[j].c > top[i].c { top[i], top[j] = top[j], top[i] } }
		}
		lim := 60; if len(top) < lim { lim = len(top) }
		for _, r := range top[:lim] {
			defs := idx.Symbols[r.n]
			k := "unknown"; if len(defs) > 0 { k = defs[0].KindStr }
			add("sym:"+r.n, r.n, k)
		}
	}
	return GraphPayload{Nodes: nodes, Edges: edges}
}

// CallGraph returns a function call/caller graph centred on root.
func (idx *Index) CallGraph(root string, depth int) GraphPayload {
	nodes   := []GraphNode{}
	edges   := []GraphEdge{}
	nodeIdx := map[string]int{}
	visited := map[string]bool{}

	add := func(id, label, kind string) int {
		if i, ok := nodeIdx[id]; ok { return i }
		i := len(nodes)
		nodes = append(nodes, GraphNode{ID: id, Label: label, Kind: kind})
		nodeIdx[id] = i
		return i
	}

	var visit func(fn string, d int)
	visit = func(fn string, d int) {
		if d < 0 || visited[fn] { return }
		visited[fn] = true
		kind := "function"
		if syms := idx.Symbols[fn]; len(syms) > 0 { kind = syms[0].KindStr }
		fnIdx := add("fn:"+fn, fn, kind)
		// Callees
		for _, e := range idx.CallEdges[fn] {
			calleeIdx := add("fn:"+e.Callee, e.Callee, "function")
			edges = append(edges, GraphEdge{Source: fnIdx, Target: calleeIdx, Weight: 1, Label: "calls"})
			if d > 0 { visit(e.Callee, d-1) }
		}
		// Callers (reverse scan — expensive but correct for small depth)
		if d > 0 {
			for callerFn, callList := range idx.CallEdges {
				for _, c := range callList {
					if c.Callee == fn && !visited[callerFn] {
						callerIdx := add("fn:"+callerFn, callerFn, "function")
						edges = append(edges, GraphEdge{Source: callerIdx, Target: fnIdx, Weight: 1, Label: "calls"})
						visit(callerFn, d-1)
					}
				}
			}
		}
	}
	visit(root, depth)
	return GraphPayload{Nodes: nodes, Edges: edges}
}
