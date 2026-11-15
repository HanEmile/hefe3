package main

import (
	"sort"
	"strconv"
	"strings"
)

// ============================================================================
// maptree.go — URL-path prefix trie for the Map v2 icicle / treemap.
//
// We tokenize each page node's templated path (pathTemplate already collapses
// numeric/uuid/long-token segments to ":id") into segments and insert it into
// a prefix trie. Each trie node is a ClusterNode. A depth-first traversal
// assigns every LEAF a contiguous slot in a global leaf-order array; an
// internal node's [LeafLo,LeafHi) range therefore covers exactly the leaves in
// its subtree. Status mix / flow count / error rate are rolled up bottom-up.
//
// The long tail (many distinct 1-hit 4xx leaves under a common prefix) folds
// naturally: the parent prefix carries the aggregate counts so callers can
// collapse it into a single "+N more" summary row when a render budget is hit.
//
// Layout fields (X,Y,R) are populated later by maplayout.go.
// ============================================================================

// ClusterNode is one node in the URL-path prefix trie.
type ClusterNode struct {
	Path     string         // full templated path prefix, e.g. "/order/:id"
	Label    string         // last path segment (display name), e.g. ":id"
	Depth    int            // 0 = root's direct children are depth 0
	Parent   *ClusterNode   // nil for synthetic root
	Children []*ClusterNode // sorted by Label for determinism

	// DFS-contiguous leaf range [LeafLo, LeafHi). For a leaf, LeafHi==LeafLo+1.
	LeafLo int
	LeafHi int

	LeafCount int    // number of leaf descendants (LeafHi-LeafLo)
	FlowCount int    // total flows folded into this subtree
	StatusMix StatusMix
	ErrorRate float64 // FlowCount==0 -> 0; else (4xx+5xx+err)/FlowCount

	// Layout (set by maplayout.go). World units.
	X, Y, R float32

	// Representative page identity for this node. For a leaf these come from the
	// page that terminated here; for internal nodes they come from a
	// representative descendant (first leaf in DFS order).
	SampleFlowID uint64
	Kind         string // html|json|redirect|error|other (representative)
	PageID       string // the BuildGraph page node id ("page:GET /x"), leaves only

	// isLeaf is true when a page actually terminated at this exact path (it may
	// still have children if a longer path shares this prefix; in that case we
	// still treat the terminal here as contributing a leaf slot).
	isLeaf bool
}

// StatusMix counts flows by HTTP status class within a subtree.
type StatusMix struct {
	C2  int `json:"c2"`
	C3  int `json:"c3"`
	C4  int `json:"c4"`
	C5  int `json:"c5"`
	Err int `json:"err"` // status==0 (no response / connection error)
}

func (m *StatusMix) add(o StatusMix) {
	m.C2 += o.C2
	m.C3 += o.C3
	m.C4 += o.C4
	m.C5 += o.C5
	m.Err += o.Err
}

func (m *StatusMix) addStatus(status, n int) {
	switch {
	case status == 0:
		m.Err += n
	case status >= 200 && status < 300:
		m.C2 += n
	case status >= 300 && status < 400:
		m.C3 += n
	case status >= 400 && status < 500:
		m.C4 += n
	default:
		m.C5 += n
	}
}

// ClusterTree is the full prefix trie plus the DFS leaf order.
type ClusterTree struct {
	Root  *ClusterNode   // synthetic root (Path "", Depth -1)
	Leaves []*ClusterNode // DFS leaf order; index == leaf slot
	byPath map[string]*ClusterNode
	MaxDepth int
}

// leafAgg accumulates the per-leaf observations before the tree is finalized.
type leafAgg struct {
	flowCount    int
	statusMix    StatusMix
	errFlows     int // 4xx+5xx+err flow count, for error rate
	sampleFlowID uint64
	kind         string
	pageID       string
	method       string
}

// TreeRow is the JSON row shape consumed by /map/tree, /map/search. It mirrors
// the contract's row definition exactly.
type TreeRow struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	Depth        int       `json:"depth"`
	Path         string    `json:"path"`
	LeafCount    int       `json:"leafCount"`
	FlowCount    int       `json:"flowCount"`
	StatusMix    StatusMix `json:"statusMix"`
	ErrorRate    float64   `json:"errorRate"`
	HasChildren  bool      `json:"hasChildren"`
	ChildCount   int       `json:"childCount"`
	SampleFlowID uint64    `json:"sampleFlowId"`
	Kind         string    `json:"kind"`
}

// BuildClusterTree builds the prefix trie from the flows. It tokenizes each
// flow's templated path, accumulates per-terminal-path observations, then
// finalizes the trie (children sort, DFS leaf numbering, bottom-up rollups).
//
// A "leaf" in this tree is any path that at least one flow terminated at. A
// path can be both a terminal (a flow ended exactly here) and an internal
// prefix (longer paths exist under it); in that case it still occupies a leaf
// slot so its own flows are not lost.
func BuildClusterTree(flows []*PersistedFlow) *ClusterTree {
	t := &ClusterTree{
		Root:   &ClusterNode{Path: "", Label: "", Depth: -1},
		byPath: map[string]*ClusterNode{"": nil},
	}

	// Per-terminal-path aggregation keyed by the full templated path. We key on
	// the path only (status-agnostic) to mirror BuildGraph's status-agnostic
	// "page" node; the status mix is rolled into StatusMix.
	aggs := map[string]*leafAgg{}

	for _, pf := range flows {
		tmpl := pathTemplate(pf.url)
		method := pf.method
		if method == "" {
			method = "GET"
		}
		var kind string
		var isErr bool
		if pf.fingerprint != "" {
			_, kind, isErr = unpackFingerprint(pf.fingerprint)
		} else {
			kind = "other"
			isErr = pf.status >= 400 || pf.status == 0
		}

		a := aggs[tmpl]
		if a == nil {
			a = &leafAgg{sampleFlowID: pf.id, kind: kind, method: method,
				pageID: "page:" + method + " " + tmpl}
			aggs[tmpl] = a
		}
		a.flowCount++
		a.statusMix.addStatus(pf.status, 1)
		if pf.status >= 400 || pf.status == 0 {
			a.errFlows++
		}
		// Keep an error-kind hint if any sample is an error and we have none yet.
		if a.kind == "" {
			a.kind = kind
		}
		if isErr && a.kind != "error" {
			// don't clobber a more specific content kind purely for one error;
			// but prefer error kind if the representative sample is an error.
		}
	}

	// Insert each terminal path into the trie, marking the terminal node a leaf
	// and attaching its aggregate. Sort terminal paths for deterministic
	// construction order (does not affect final DFS order which re-sorts).
	paths := make([]string, 0, len(aggs))
	for p := range aggs {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		node := t.insert(p)
		node.isLeaf = true
		a := aggs[p]
		node.SampleFlowID = a.sampleFlowID
		node.Kind = a.kind
		node.PageID = a.pageID
		node.FlowCount = a.flowCount
		node.StatusMix = a.statusMix
	}

	t.finalize()
	return t
}

// insert walks/creates the trie path for a templated URL path and returns the
// terminal node. Segments are the path components; the root holds the leading
// "/" implicitly.
func (t *ClusterTree) insert(tmpl string) *ClusterNode {
	segs := splitSegments(tmpl)
	cur := t.Root
	prefix := ""
	for _, seg := range segs {
		prefix = prefix + "/" + seg
		child := findChild(cur, seg)
		if child == nil {
			child = &ClusterNode{
				Path:   prefix,
				Label:  seg,
				Depth:  cur.Depth + 1,
				Parent: cur,
			}
			cur.Children = append(cur.Children, child)
			t.byPath[prefix] = child
		}
		cur = child
	}
	// The "/" root case: a flow to "/" has no segments; represent it as a single
	// synthetic child labelled "/".
	if len(segs) == 0 {
		child := findChild(cur, "/")
		if child == nil {
			child = &ClusterNode{Path: "/", Label: "/", Depth: cur.Depth + 1, Parent: cur}
			cur.Children = append(cur.Children, child)
			t.byPath["/"] = child
		}
		cur = child
	}
	return cur
}

// splitSegments splits a templated path into non-empty segments.
func splitSegments(tmpl string) []string {
	trimmed := strings.Trim(tmpl, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func findChild(n *ClusterNode, label string) *ClusterNode {
	for _, c := range n.Children {
		if c.Label == label {
			return c
		}
	}
	return nil
}

// finalize sorts children, assigns DFS-contiguous leaf slots, and rolls up
// counts bottom-up.
func (t *ClusterTree) finalize() {
	// Stable child ordering for determinism.
	var sortRec func(n *ClusterNode)
	sortRec = func(n *ClusterNode) {
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Label < n.Children[j].Label })
		for _, c := range n.Children {
			sortRec(c)
		}
	}
	sortRec(t.Root)

	// Bucket the long tail: at any node with too many childless-leaf children
	// (e.g. thousands of distinct scan-probe 404s), fold those leaves into a few
	// synthetic per-status-class summary nodes ("4xx · 8213 paths") so the icicle
	// shows a handful of legible rows instead of thousands. The summary nodes are
	// expandable (the original leaves become their children), so nothing is lost.
	t.bucketize(t.Root)

	// DFS leaf numbering + rollups. A node contributes a leaf slot iff it is a
	// terminal (isLeaf) OR it has no children at all. Internal terminals also get
	// their own slot so their flows are represented as a leaf.
	t.Leaves = t.Leaves[:0]
	var dfs func(n *ClusterNode)
	dfs = func(n *ClusterNode) {
		n.LeafLo = len(t.Leaves)
		if n.Depth > t.MaxDepth {
			t.MaxDepth = n.Depth
		}

		// A terminal node (a flow ended exactly here) occupies its own leaf slot,
		// regardless of whether it also has children.
		ownMix := n.StatusMix
		ownFlows := n.FlowCount
		if n.isLeaf {
			t.Leaves = append(t.Leaves, n)
		}

		if len(n.Children) == 0 {
			// Pure leaf (no descendants). If it wasn't a terminal (shouldn't happen
			// because every node is created along a terminal path), still give it a
			// slot so the range is non-empty.
			if !n.isLeaf {
				t.Leaves = append(t.Leaves, n)
			}
			n.LeafHi = len(t.Leaves)
			n.LeafCount = n.LeafHi - n.LeafLo
			n.computeErrorRate()
			return
		}

		// Internal node: recurse, accumulating child rollups.
		var roll StatusMix
		roll.add(ownMix)
		rollFlows := ownFlows
		for _, c := range n.Children {
			dfs(c)
			roll.add(c.StatusMix)
			rollFlows += c.FlowCount
		}
		n.StatusMix = roll
		n.FlowCount = rollFlows
		n.LeafHi = len(t.Leaves)
		n.LeafCount = n.LeafHi - n.LeafLo
		n.computeErrorRate()

		// Representative sample/kind for an internal node: inherit from the first
		// leaf in its subtree if it has none of its own.
		if n.SampleFlowID == 0 && n.LeafLo < len(t.Leaves) {
			rep := t.Leaves[n.LeafLo]
			n.SampleFlowID = rep.SampleFlowID
			if n.Kind == "" {
				n.Kind = rep.Kind
			}
		}
	}
	// Walk roots (children of synthetic root).
	t.Leaves = nil
	for _, c := range t.Root.Children {
		dfs(c)
	}
	// Root range covers all leaves.
	t.Root.LeafLo = 0
	t.Root.LeafHi = len(t.Leaves)
	t.Root.LeafCount = len(t.Leaves)
	var rootMix StatusMix
	rootFlows := 0
	for _, c := range t.Root.Children {
		rootMix.add(c.StatusMix)
		rootFlows += c.FlowCount
	}
	t.Root.StatusMix = rootMix
	t.Root.FlowCount = rootFlows
	t.Root.computeErrorRate()
}

func (n *ClusterNode) computeErrorRate() {
	if n.FlowCount == 0 {
		n.ErrorRate = 0
		return
	}
	errs := n.StatusMix.C4 + n.StatusMix.C5 + n.StatusMix.Err
	n.ErrorRate = float64(errs) / float64(n.FlowCount)
}

// bucketThreshold: only fold childless-leaf children into status buckets when a
// node has at least this many of them. Below this, the raw rows are fine.
const bucketThreshold = 16

// statusClassLabel maps a leaf's representative status to a bucket key/label.
func leafStatusClass(c *ClusterNode) (key, label string, order int) {
	m := c.StatusMix
	switch {
	case m.C5 > 0:
		return "5xx", "5xx", 5
	case m.Err > 0:
		return "err", "err (no response)", 6
	case m.C4 > 0:
		return "4xx", "4xx", 4
	case m.C3 > 0:
		return "3xx", "3xx", 3
	case m.C2 > 0:
		return "2xx", "2xx", 2
	default:
		return "other", "other", 7
	}
}

// bucketize recursively folds the childless-leaf children of each node into
// synthetic per-status-class summary buckets when there are many of them.
// Buckets are registered in byPath (path "<parent>/§<class>") and are expandable
// (their Children are the original leaves), so drill-down still reaches every page.
func (t *ClusterTree) bucketize(n *ClusterNode) {
	// Recurse first into genuine structural children (those with their own kids),
	// then decide whether to bucket this node's flat-leaf children.
	for _, c := range n.Children {
		if len(c.Children) > 0 {
			t.bucketize(c)
		}
	}

	// Partition this node's children: keep structural children (have kids) as-is;
	// collect childless leaves as bucketing candidates.
	var structural []*ClusterNode
	var leaves []*ClusterNode
	for _, c := range n.Children {
		if len(c.Children) > 0 {
			structural = append(structural, c)
		} else {
			leaves = append(leaves, c)
		}
	}
	if len(leaves) < bucketThreshold {
		return // not worth bucketing
	}

	// Group leaves by status class.
	type bucket struct {
		key, label string
		order      int
		kids       []*ClusterNode
	}
	groups := map[string]*bucket{}
	var order []string
	for _, lf := range leaves {
		k, lbl, ord := leafStatusClass(lf)
		b := groups[k]
		if b == nil {
			b = &bucket{key: k, label: lbl, order: ord}
			groups[k] = b
			order = append(order, k)
		}
		b.kids = append(b.kids, lf)
	}

	// If everything collapses to a single class with few entries it's not worth
	// a wrapper, but with >=bucketThreshold leaves we always bucket for legibility.
	sort.Slice(order, func(i, j int) bool { return groups[order[i]].order < groups[order[j]].order })

	newChildren := structural
	for _, k := range order {
		b := groups[k]
		bnode := &ClusterNode{
			Path:   n.Path + "/§" + b.key,
			Label:  b.label + " · " + itoa(len(b.kids)) + " paths",
			Depth:  n.Depth + 1,
			Parent: n,
		}
		// Reparent the leaves under the bucket and fix their depth.
		for _, lf := range b.kids {
			lf.Parent = bnode
			bumpDepth(lf, bnode.Depth+1-lf.Depth)
		}
		bnode.Children = b.kids
		newChildren = append(newChildren, bnode)
		t.byPath[bnode.Path] = bnode
	}
	n.Children = newChildren
}

// bumpDepth adjusts a subtree's Depth by delta (used when reparenting leaves
// under a synthetic bucket node).
func bumpDepth(n *ClusterNode, delta int) {
	n.Depth += delta
	for _, c := range n.Children {
		bumpDepth(c, delta)
	}
}

// itoa is a tiny strconv.Itoa wrapper kept local to avoid an import churn.
func itoa(i int) string { return strconv.Itoa(i) }

// row converts a ClusterNode into a TreeRow.
func (n *ClusterNode) row() TreeRow {
	return TreeRow{
		ID:           n.Path,
		Label:        n.Label,
		Depth:        n.Depth,
		Path:         n.Path,
		LeafCount:    n.LeafCount,
		FlowCount:    n.FlowCount,
		StatusMix:    n.StatusMix,
		ErrorRate:    n.ErrorRate,
		HasChildren:  len(n.Children) > 0,
		ChildCount:   len(n.Children),
		SampleFlowID: n.SampleFlowID,
		Kind:         n.Kind,
	}
}

// RootRows returns the top-level rows (direct children of the synthetic root).
func (t *ClusterTree) RootRows() []TreeRow {
	rows := make([]TreeRow, 0, len(t.Root.Children))
	for _, c := range t.Root.Children {
		rows = append(rows, c.row())
	}
	return rows
}

// ChildrenOf returns the direct children rows of the node at path. path=="" is
// equivalent to RootRows(). Unknown path returns an empty slice.
func (t *ClusterTree) ChildrenOf(path string) []TreeRow {
	if path == "" {
		return t.RootRows()
	}
	n := t.byPath[path]
	if n == nil {
		return []TreeRow{}
	}
	rows := make([]TreeRow, 0, len(n.Children))
	for _, c := range n.Children {
		rows = append(rows, c.row())
	}
	return rows
}

// Node returns the ClusterNode at the given path (nil if absent). The synthetic
// root is never returned for "" — callers use RootRows for that.
func (t *ClusterTree) Node(path string) *ClusterNode {
	if path == "" {
		return nil
	}
	return t.byPath[path]
}

// SearchLeaves runs a query and returns matching rows plus the set of ancestor
// path prefixes the UI should auto-expand to reveal them. Query forms:
//
//	flow:<id>     match the leaf whose SampleFlowID == id
//	status:NNN    match leaves whose StatusMix has a flow in that status' class
//	error:*       match leaves with ErrorRate > 0
//	<substring>   case-insensitive substring match on the path
//
// Results are capped to keep the response bounded.
func (t *ClusterTree) SearchLeaves(q string) (matches []TreeRow, expand []string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	const maxMatches = 500

	expandSet := map[string]bool{}
	addMatch := func(n *ClusterNode) bool {
		matches = append(matches, n.row())
		for p := n.Parent; p != nil && p.Depth >= 0; p = p.Parent {
			expandSet[p.Path] = true
		}
		return len(matches) < maxMatches
	}

	switch {
	case strings.HasPrefix(q, "flow:"):
		idStr := strings.TrimSpace(q[len("flow:"):])
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err == nil {
			for _, leaf := range t.Leaves {
				if leaf.SampleFlowID == id {
					if !addMatch(leaf) {
						break
					}
				}
			}
		}
	case strings.HasPrefix(q, "status:"):
		codeStr := strings.TrimSpace(q[len("status:"):])
		code, err := strconv.Atoi(codeStr)
		if err == nil {
			for _, leaf := range t.Leaves {
				if leafHasStatusClass(leaf, code) {
					if !addMatch(leaf) {
						break
					}
				}
			}
		}
	case q == "error:*" || strings.HasPrefix(q, "error:"):
		for _, leaf := range t.Leaves {
			if leaf.ErrorRate > 0 {
				if !addMatch(leaf) {
					break
				}
			}
		}
	default:
		needle := strings.ToLower(q)
		for _, leaf := range t.Leaves {
			if strings.Contains(strings.ToLower(leaf.Path), needle) {
				if !addMatch(leaf) {
					break
				}
			}
		}
	}

	expand = make([]string, 0, len(expandSet))
	for p := range expandSet {
		expand = append(expand, p)
	}
	sort.Strings(expand)
	return matches, expand
}

// leafHasStatusClass reports whether a leaf has any flow in the status class of
// the given code (e.g. 404 -> any 4xx flow; 0 -> connection errors).
func leafHasStatusClass(n *ClusterNode, code int) bool {
	switch {
	case code == 0:
		return n.StatusMix.Err > 0
	case code >= 200 && code < 300:
		return n.StatusMix.C2 > 0
	case code >= 300 && code < 400:
		return n.StatusMix.C3 > 0
	case code >= 400 && code < 500:
		return n.StatusMix.C4 > 0
	default:
		return n.StatusMix.C5 > 0
	}
}
