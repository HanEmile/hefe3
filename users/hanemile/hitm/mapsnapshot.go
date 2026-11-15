package main

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ============================================================================
// mapsnapshot.go — immutable Map v2 snapshot + single-flight store.
//
// A MapSnapshot is an immutable, columnar-packed view of the state map built
// from all persisted flows at a particular s.mapGen. The MapStore holds the
// current snapshot in an atomic.Pointer and rebuilds lazily (single-flight)
// whenever s.mapGen advances. Readers always get a consistent, never-changing
// snapshot; a rebuild publishes a new one atomically. During a rebuild the
// previous snapshot keeps being served (or "not ready" if none exists yet).
//
// ============================================================================
// INTERFACE EXPOSED TO TRACK B (mapwire.go / maphandlers.go)
// ============================================================================
//
// Entry point:
//   func (s *HITM) mapSnapshot() (*MapSnapshot, bool)
//       Returns the current snapshot and whether one is ready. Triggers a
//       background single-flight rebuild if s.mapGen advanced. Never blocks on
//       a build in progress; serves the previous snapshot meanwhile. The bool
//       is false only before the very first build completes (handler should
//       reply 202).
//
// On *MapSnapshot (all read-only, safe for concurrent use):
//   .Gen      uint64  (exported field, consumed directly as snap.Gen)
//   .Building bool    (exported field)
//   Meta() MapMeta                                   // gen/nodeN/edgeN/clusterN/worldBBox/maxDepth
//   RootRows() []TreeRow                             // icicle roots
//   ChildrenOf(path string) []TreeRow                // lazy icicle expand
//   SearchLeaves(q string) (matches []TreeRow, expand []string)
//   Neighborhood(nodeID string, hops int) ([]*MapNode, []*MapEdge)  // <=50 nodes
//   ClusterColumns(depth int) ClusterColumns          // value; HMAP source cols for /map/clusters
//   Labels(ids []string) map[string]LabelInfo         // lazy hover/detail
//
// MapMeta has: Gen, Building, NodeN, EdgeN, ClusterN int/uint64/bool; WorldBBox
// [4]float32; MaxDepth int. (JSON-tagged for direct marshaling.)
//
// ClusterColumns carries the parallel arrays Track B's HMAP encoder needs
// (returned by value): Gen uint32; X,Y,R []float32; Color []uint32;
// NType []uint8; Status []int16; ESrc,EDst []uint32; EColor []uint32;
// EFlags []uint8. Node order is dense; ESrc/EDst index node columns directly.
//
// LabelInfo is the per-id record returned by Labels():
//   Label, Path string; Status int; Kind string; Count int; SampleFlowID uint64
//
// Track B owns mapwire.go (HMAP byte encoding) and maphandlers.go (route
// registration). This file does NOT encode HMAP or register routes; it only
// produces the columns and JSON-shaped data.
// ============================================================================

// Status colors (packed 0xRRGGBBAA) per the contract.
const (
	colorC2  uint32 = 0x1f7a3fff // 2xx green
	colorC3  uint32 = 0x5b8db8ff // 3xx blue
	colorC4  uint32 = 0xcc7a00ff // 4xx amber
	colorC5  uint32 = 0xff5555ff // 5xx / err red
	colorMix uint32 = 0x888888ff // mixed / neutral cluster
)

// worldWidth/worldHeight define the layout box. The treemap is square-ish; a
// square keeps aspect ratios balanced.
const (
	mapWorldW = 4096.0
	mapWorldH = 4096.0
)

// MapMeta is the /map/meta JSON payload.
type MapMeta struct {
	Gen       uint64     `json:"gen"`
	Building  bool       `json:"building"`
	NodeN     int        `json:"nodeN"`
	EdgeN     int        `json:"edgeN"`
	ClusterN  int        `json:"clusterN"`
	WorldBBox [4]float32 `json:"worldBBox"`
	MaxDepth  int        `json:"maxDepth"`
}

// LabelInfo is the lazy hover/detail record returned by Labels(), keyed by id.
type LabelInfo struct {
	Label        string `json:"label"`
	Path         string `json:"path"`
	Status       int    `json:"status"`
	Kind         string `json:"kind"`
	Count        int    `json:"count"`
	SampleFlowID uint64 `json:"sampleFlowId"`
}

// ClusterColumns holds the parallel columnar arrays for the Overview
// cluster-DAG, ready for HMAP encoding by Track B. Returned by value from
// ClusterColumns(); the slices are shared/immutable.
type ClusterColumns struct {
	Gen uint32

	// Node columns (dense; index i is one cluster bubble).
	X      []float32
	Y      []float32
	R      []float32
	Color  []uint32
	NType  []uint8
	Status []int16

	// Edge columns (ESrc/EDst are dense node indices into the columns above).
	ESrc   []uint32
	EDst   []uint32
	EColor []uint32
	EFlags []uint8
}

// MapSnapshot is an immutable built view of the map at a fixed generation.
// Gen and Building are exported fields consumed directly by Track B's handlers
// (snap.Gen, snap.Building).
type MapSnapshot struct {
	Gen      uint64 // generation this snapshot was built at
	Building bool   // transient "first build in progress" placeholder flag

	tree  *ClusterTree
	graph *MapGraph // the flat BuildGraph result, for Neighborhood

	// Node index for Neighborhood lookups.
	nodeByID map[string]*MapNode
	// adjacency: nodeID -> edges touching it (both directions)
	adjacency map[string][]*MapEdge

	worldBBox [4]float32

	// Cached cluster columns per depth (computed lazily, guarded by clusterMu).
	clusterMu    sync.Mutex
	clusterCache map[int]*ClusterColumns
}

// Meta returns the /map/meta payload for this snapshot.
func (m *MapSnapshot) Meta() MapMeta {
	clusterN := 0
	if m.tree != nil && m.tree.Root != nil {
		clusterN = countInternal(m.tree.Root)
	}
	return MapMeta{
		Gen:       m.Gen,
		Building:  m.Building,
		NodeN:     len(m.graph.Nodes),
		EdgeN:     len(m.graph.Edges),
		ClusterN:  clusterN,
		WorldBBox: m.worldBBox,
		MaxDepth:  m.tree.MaxDepth,
	}
}

// countInternal counts trie nodes that have children (cluster bubbles).
func countInternal(n *ClusterNode) int {
	count := 0
	if len(n.Children) > 0 && n.Depth >= 0 {
		count = 1
	}
	for _, c := range n.Children {
		count += countInternal(c)
	}
	return count
}

// RootRows returns the icicle root rows.
func (m *MapSnapshot) RootRows() []TreeRow { return m.tree.RootRows() }

// ChildrenOf returns the direct children rows under path.
func (m *MapSnapshot) ChildrenOf(path string) []TreeRow { return m.tree.ChildrenOf(path) }

// SearchLeaves runs a query against the tree.
func (m *MapSnapshot) SearchLeaves(q string) ([]TreeRow, []string) { return m.tree.SearchLeaves(q) }

// Neighborhood returns up to 50 nodes within `hops` of nodeID, plus the edges
// among them, in the existing MapNode/MapEdge JSON shapes (for the reused force
// graph). nodeID may be a page node id ("page:GET /x"), a status node id
// ("page:GET /x#200"), OR a cluster tree path ("/order/:id"). When a tree path
// is given we map it to that node's PageID (its representative page node).
func (m *MapSnapshot) Neighborhood(nodeID string, hops int) ([]*MapNode, []*MapEdge) {
	if hops < 1 {
		hops = 1
	}
	const maxNodes = 50

	// Resolve a tree path to its page node id if necessary.
	start := nodeID
	if _, ok := m.nodeByID[start]; !ok {
		if cn := m.tree.Node(nodeID); cn != nil && cn.PageID != "" {
			start = cn.PageID
		}
	}
	if _, ok := m.nodeByID[start]; !ok {
		return nil, nil
	}

	// BFS over the undirected adjacency.
	visited := map[string]bool{start: true}
	frontier := []string{start}
	for h := 0; h < hops && len(visited) < maxNodes; h++ {
		var next []string
		for _, id := range frontier {
			for _, e := range m.adjacency[id] {
				other := e.To
				if other == id {
					other = e.From
				}
				if !visited[other] {
					if len(visited) >= maxNodes {
						break
					}
					visited[other] = true
					next = append(next, other)
				}
			}
			if len(visited) >= maxNodes {
				break
			}
		}
		frontier = next
	}

	nodes := make([]*MapNode, 0, len(visited))
	for id := range visited {
		if n := m.nodeByID[id]; n != nil {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	// Edges with both endpoints inside the visited set.
	seen := map[*MapEdge]bool{}
	var edges []*MapEdge
	for id := range visited {
		for _, e := range m.adjacency[id] {
			if seen[e] {
				continue
			}
			if visited[e.From] && visited[e.To] {
				seen[e] = true
				edges = append(edges, e)
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return nodes, edges
}

// Labels returns lazy label/detail records for the given ids. ids may be tree
// paths (cluster nodes) or flat graph node ids; both are resolved.
func (m *MapSnapshot) Labels(ids []string) map[string]LabelInfo {
	out := make(map[string]LabelInfo, len(ids))
	for _, id := range ids {
		if cn := m.tree.Node(id); cn != nil {
			out[id] = LabelInfo{
				Label:        cn.Label,
				Path:         cn.Path,
				Status:       representativeStatus(cn.StatusMix),
				Kind:         cn.Kind,
				Count:        cn.FlowCount,
				SampleFlowID: cn.SampleFlowID,
			}
			continue
		}
		if n := m.nodeByID[id]; n != nil {
			out[id] = LabelInfo{
				Label:        n.Label,
				Path:         n.Label,
				Status:       n.Status,
				Kind:         n.Kind,
				Count:        n.Count,
				SampleFlowID: n.SampleFlowID,
			}
		}
	}
	return out
}

// representativeStatus picks a representative status code from a mix (the most
// severe class that has flows), for label display.
func representativeStatus(mix StatusMix) int {
	switch {
	case mix.Err > 0:
		return 0
	case mix.C5 > 0:
		return 500
	case mix.C4 > 0:
		return 400
	case mix.C3 > 0:
		return 300
	case mix.C2 > 0:
		return 200
	}
	return 0
}

// ClusterColumns returns the columnar cluster-bubble + cluster-edge arrays for
// the Overview at the given depth (clusters at depth <= `depth` that are the
// deepest visible; aggregated edges between visible clusters). Results are
// cached per depth on the snapshot. depth<1 is treated as 1. Returned by value;
// the underlying slices are shared/immutable, so callers must not mutate them.
func (m *MapSnapshot) ClusterColumns(depth int) ClusterColumns {
	if depth < 1 {
		depth = 1
	}
	m.clusterMu.Lock()
	defer m.clusterMu.Unlock()
	if c, ok := m.clusterCache[depth]; ok {
		return *c
	}
	c := m.buildClusterColumns(depth)
	m.clusterCache[depth] = c
	return *c
}

// buildClusterColumns walks the tree, collecting the "visible" cluster nodes:
// a node is visible if its Depth <= depth-1 (0-based depths; depth=2 shows
// depths 0 and 1) AND (it has no children OR its depth == depth-1). We map each
// page/status flat edge's endpoints up to the visible cluster that contains
// them, and aggregate cluster->cluster edges.
func (m *MapSnapshot) buildClusterColumns(depth int) *ClusterColumns {
	maxVisibleDepth := depth - 1 // 0-based

	// Collect visible cluster nodes and assign dense indices.
	var visible []*ClusterNode
	idxByPath := map[string]int{}
	var collect func(n *ClusterNode)
	collect = func(n *ClusterNode) {
		for _, c := range n.Children {
			isVisible := c.Depth == maxVisibleDepth || len(c.Children) == 0 && c.Depth <= maxVisibleDepth
			if c.Depth <= maxVisibleDepth {
				if isVisible {
					idxByPath[c.Path] = len(visible)
					visible = append(visible, c)
				} else {
					collect(c)
				}
			}
		}
	}
	collect(m.tree.Root)

	set := &ClusterColumns{
		Gen:    uint32(m.Gen),
		X:      make([]float32, len(visible)),
		Y:      make([]float32, len(visible)),
		R:      make([]float32, len(visible)),
		Color:  make([]uint32, len(visible)),
		NType:  make([]uint8, len(visible)),
		Status: make([]int16, len(visible)),
	}
	for i, c := range visible {
		set.X[i] = c.X
		set.Y[i] = c.Y
		set.R[i] = c.R
		set.Color[i] = clusterColor(c.StatusMix)
		set.NType[i] = 2 // cluster bubble
		set.Status[i] = int16(representativeStatus(c.StatusMix))
	}

	// Map a flat graph node id to the index of the visible cluster containing it.
	// Flat page/status nodes carry a path template; we ascend the trie from that
	// path to the nearest visible ancestor.
	clusterIndexForFlatNode := func(n *MapNode) (int, bool) {
		path := pageNodePath(n)
		if path == "" {
			return 0, false
		}
		// Find the deepest visible ancestor (including self) of `path`.
		cn := m.tree.Node(path)
		for cn != nil && cn.Depth >= 0 {
			if i, ok := idxByPath[cn.Path]; ok {
				return i, true
			}
			cn = cn.Parent
		}
		return 0, false
	}

	// Aggregate cluster->cluster edges from the flat transition edges.
	type edgeKey struct{ a, b int }
	edgeAgg := map[edgeKey]*struct {
		count int
		async bool
		err   bool
	}{}
	order := []edgeKey{}
	for _, e := range m.graph.Edges {
		from := m.nodeByID[e.From]
		to := m.nodeByID[e.To]
		if from == nil || to == nil {
			continue
		}
		ci, ok1 := clusterIndexForFlatNode(from)
		cj, ok2 := clusterIndexForFlatNode(to)
		if !ok1 || !ok2 || ci == cj {
			continue // self-loops within one cluster are not drawn at this LOD
		}
		k := edgeKey{ci, cj}
		agg := edgeAgg[k]
		if agg == nil {
			agg = &struct {
				count int
				async bool
				err   bool
			}{}
			edgeAgg[k] = agg
			order = append(order, k)
		}
		agg.count += e.Count
		agg.async = agg.async || e.Async
		agg.err = agg.err || e.Error
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].a != order[j].a {
			return order[i].a < order[j].a
		}
		return order[i].b < order[j].b
	})
	for _, k := range order {
		agg := edgeAgg[k]
		set.ESrc = append(set.ESrc, uint32(k.a))
		set.EDst = append(set.EDst, uint32(k.b))
		set.EColor = append(set.EColor, edgeColor(agg.err))
		var flags uint8
		if agg.async {
			flags |= 0x01
		}
		if agg.err {
			flags |= 0x02
		}
		flags |= 0x04 // transition edge renders
		set.EFlags = append(set.EFlags, flags)
	}
	return set
}

// pageNodePath extracts the templated path from a flat MapNode id. Page node
// ids are "page:METHOD /path"; status node ids are "page:METHOD /path#NNN".
func pageNodePath(n *MapNode) string {
	id := n.ID
	if !strings.HasPrefix(id, "page:") {
		return ""
	}
	rest := id[len("page:"):]
	// strip "#status" suffix if present
	if h := strings.IndexByte(rest, '#'); h >= 0 {
		rest = rest[:h]
	}
	// rest is "METHOD /path"; take the path after the first space.
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		return rest[sp+1:]
	}
	return rest
}

// clusterColor picks a bubble color from a status mix (dominant class).
func clusterColor(mix StatusMix) uint32 {
	total := mix.C2 + mix.C3 + mix.C4 + mix.C5 + mix.Err
	if total == 0 {
		return colorMix
	}
	// Error-leaning clusters dominate the color for visibility.
	if mix.C5+mix.Err > 0 && (mix.C5+mix.Err)*4 >= total {
		return colorC5
	}
	if mix.C4 > 0 && mix.C4*2 >= total {
		return colorC4
	}
	max := mix.C2
	c := colorC2
	if mix.C3 > max {
		max, c = mix.C3, colorC3
	}
	if mix.C4 > max {
		max, c = mix.C4, colorC4
	}
	if mix.C5 > max {
		max, c = mix.C5, colorC5
	}
	if mix.Err > max {
		c = colorC5
	}
	return c
}

func edgeColor(isErr bool) uint32 {
	if isErr {
		return colorC5
	}
	return 0xaaaaaaff
}

// ============================================================================
// MapStore: atomic snapshot holder with single-flight rebuild.
// ============================================================================

// MapStore holds the current MapSnapshot and coordinates lazy, single-flight
// rebuilds keyed to a generation counter.
type MapStore struct {
	cur      atomic.Pointer[MapSnapshot]
	building atomic.Bool // a rebuild is in flight
	mu       sync.Mutex  // serializes the decision to start a build
}

// NewMapStore constructs an empty MapStore (no snapshot yet).
func NewMapStore() *MapStore { return &MapStore{} }

// snapshot returns (current, ready). It triggers a background rebuild if the
// current snapshot is missing or stale relative to `gen`. It never blocks on a
// build in progress: it returns the previous snapshot (ready=true) or
// (nil,false) if none exists yet. The build runs via buildFn under a
// single-flight guard.
func (ms *MapStore) snapshot(gen uint64, buildFn func(gen uint64) *MapSnapshot) (*MapSnapshot, bool) {
	cur := ms.cur.Load()
	if cur != nil && cur.Gen == gen {
		return cur, true
	}

	// Stale or missing: try to start a single-flight rebuild.
	ms.mu.Lock()
	cur = ms.cur.Load()
	if cur != nil && cur.Gen == gen {
		ms.mu.Unlock()
		return cur, true
	}
	if ms.building.Load() {
		// Another goroutine is building; serve previous (or not-ready).
		ms.mu.Unlock()
		return cur, cur != nil
	}
	ms.building.Store(true)
	ms.mu.Unlock()

	// If we have no snapshot at all, build synchronously so the first request
	// gets data (callers that don't want to block can check ready first). If we
	// already have a (stale) snapshot, build in the background and serve stale.
	if cur == nil {
		snap := buildFn(gen)
		ms.cur.Store(snap)
		ms.building.Store(false)
		return snap, true
	}

	go func() {
		defer ms.building.Store(false)
		snap := buildFn(gen)
		// Only publish if still the freshest build (gen may have advanced again;
		// that's fine — a later rebuild will supersede).
		ms.cur.Store(snap)
	}()
	return cur, true
}

// ============================================================================
// HITM hook
// ============================================================================

// mapSnapshot returns the current immutable map snapshot for the latest
// generation, building it (single-flight) if stale. The returned bool is true
// once any snapshot is available. Track B's handlers call this and reply 202 if
// ready==false.
//
// The HITM struct gains a `mapStore *MapStore` field, initialized in main().
func (s *HITM) mapSnapshot() (*MapSnapshot, bool) {
	if s.mapStore == nil {
		// Defensive: if not wired up, lazily create it (no-op once set in main()).
		s.mapStore = NewMapStore()
	}
	gen := s.mapGen.Load()
	return s.mapStore.snapshot(gen, s.buildMapSnapshot)
}

// buildMapSnapshot loads flow metadata and builds an immutable snapshot at the
// given generation: flat BuildGraph (for the neighborhood pane), the cluster
// trie (maptree.go), and the deterministic layout (maplayout.go).
func (s *HITM) buildMapSnapshot(gen uint64) *MapSnapshot {
	flows, err := s.Store.LoadFlowsMeta()
	if err != nil {
		// On error, publish an empty-but-valid snapshot so handlers can serve
		// meta/empty rows rather than spinning.
		flows = nil
	}

	graph := BuildGraph(flows)
	tree := BuildClusterTree(flows)
	bbox := LayoutTree(tree, mapWorldW, mapWorldH)

	snap := &MapSnapshot{
		Gen:          gen,
		tree:         tree,
		graph:        graph,
		worldBBox:    bbox,
		nodeByID:     make(map[string]*MapNode, len(graph.Nodes)),
		adjacency:    make(map[string][]*MapEdge, len(graph.Nodes)),
		clusterCache: map[int]*ClusterColumns{},
	}
	for _, n := range graph.Nodes {
		snap.nodeByID[n.ID] = n
	}
	for _, e := range graph.Edges {
		snap.adjacency[e.From] = append(snap.adjacency[e.From], e)
		if e.To != e.From {
			snap.adjacency[e.To] = append(snap.adjacency[e.To], e)
		}
	}
	return snap
}

// statusCodeString is a tiny helper kept here (not in map.go to avoid touching
// it) for any caller that needs a code as text; unused by default but handy for
// Track B label formatting.
func statusCodeString(code int) string {
	if code == 0 {
		return "ERR"
	}
	return strconv.Itoa(code)
}
