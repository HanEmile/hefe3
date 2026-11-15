package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// ============================================================================
// HITM Map v2 — HTTP handlers (Track B)
//
// These endpoints are purely additive: the existing /map handler and its cache
// (map.go) are untouched. Every handler reads from an immutable per-generation
// snapshot owned by Track A (mapsnapshot.go) via s.mapSnapshot(). When the
// snapshot is still building and none is ready yet, we answer 202 Accepted so
// the frontend can poll; once a snapshot exists we serve it (possibly stale)
// while a newer one rebuilds in the background.
//
// Track A accessor surface this file depends on (reconciled against the real
// mapsnapshot.go / maptree.go that already landed):
//
//	(s *HITM) mapSnapshot() (snap *MapSnapshot, ready bool)
//	snap.Gen uint64                          // exported field
//	snap.Building bool                        // exported field
//	snap.Meta() MapMeta
//	snap.RootRows() []TreeRow
//	snap.ChildrenOf(path string) []TreeRow
//	snap.SearchLeaves(q string) (matches []TreeRow, expandPaths []string)
//	snap.Neighborhood(node string, hops int) (nodes []*MapNode, edges []*MapEdge)
//	snap.ClusterColumns(depth int) ClusterColumns   // value, fields X,Y,R,Color,NType,Status,ESrc,EDst,EColor,EFlags
//	snap.Labels(ids []string) map[string]LabelInfo
//
// Track A defines: MapMeta, TreeRow, StatusMix (maptree.go), ClusterColumns,
// LabelInfo (mapsnapshot.go). MapNode/MapEdge are the existing map.go shapes.
// ============================================================================

// registerMapV2 wires the additive /map/* routes into the mux. Called from
// main.go immediately after mux.HandleFunc("/map", s.mapHandler).
func (s *HITM) registerMapV2(mux *http.ServeMux) {
	mux.HandleFunc("/map/meta", s.mapMetaHandler)
	mux.HandleFunc("/map/tree", s.mapTreeHandler)
	mux.HandleFunc("/map/search", s.mapSearchHandler)
	mux.HandleFunc("/map/neighborhood", s.mapNeighborhoodHandler)
	mux.HandleFunc("/map/clusters", s.mapClustersHandler)
	mux.HandleFunc("/map/labels", s.mapLabelsHandler)
}

// snapshotOrAccepted resolves the current snapshot. If none is ready yet it
// writes 202 Accepted (with a tiny JSON body describing the building state) and
// returns ready=false so the handler can stop. The returned snapshot may be a
// previous generation serving stale during a rebuild — that is intentional per
// the contract.
func (s *HITM) snapshotOrAccepted(w http.ResponseWriter) (*MapSnapshot, bool) {
	snap, ready := s.mapSnapshot()
	if !ready || snap == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"building":true}`))
		return nil, false
	}
	return snap, true
}

// writeJSONWithETag marshals v and serves it honoring If-None-Match/ETag.
func writeJSONWithETag(w http.ResponseWriter, r *http.Request, gen uint64, endpoint, params string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeWithETag(w, r, "application/json", etagFor(gen, endpoint, params), body)
}

// GET /map/meta?gen= -> JSON MapMeta.
func (s *HITM) mapMetaHandler(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshotOrAccepted(w)
	if !ok {
		return
	}
	meta := snap.Meta()
	writeJSONWithETag(w, r, snap.Gen, "meta", "", meta)
}

// GET /map/tree?path=&depth=1 -> JSON {rows:[TreeRow...]}.
// path="" returns the roots; otherwise returns the direct children of path.
func (s *HITM) mapTreeHandler(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshotOrAccepted(w)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")

	var rows []TreeRow
	if path == "" {
		rows = snap.RootRows()
	} else {
		rows = snap.ChildrenOf(path)
	}
	if rows == nil {
		rows = []TreeRow{}
	}

	resp := struct {
		Rows []TreeRow `json:"rows"`
	}{Rows: rows}
	writeJSONWithETag(w, r, snap.Gen, "tree", "path="+path, resp)
}

// GET /map/search?gen=&q= -> JSON {matches:[TreeRow...], expandPaths:[...]}.
// q supports: plain substring on path | status:NNN | error:* | flow:<id>.
func (s *HITM) mapSearchHandler(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshotOrAccepted(w)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")

	matches, expand := snap.SearchLeaves(q)
	if matches == nil {
		matches = []TreeRow{}
	}
	if expand == nil {
		expand = []string{}
	}

	resp := struct {
		Matches     []TreeRow `json:"matches"`
		ExpandPaths []string  `json:"expandPaths"`
	}{Matches: matches, ExpandPaths: expand}
	writeJSONWithETag(w, r, snap.Gen, "search", "q="+q, resp)
}

// GET /map/neighborhood?gen=&node=&hops=1 -> JSON {nodes:[MapNode...], edges:[MapEdge...]}.
// Feeds the reused force-graph pane; capped to <=50 nodes by Track A.
func (s *HITM) mapNeighborhoodHandler(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshotOrAccepted(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	node := q.Get("node")
	if node == "" {
		http.Error(w, "missing node parameter", http.StatusBadRequest)
		return
	}
	hops := 1
	if h := q.Get("hops"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n >= 0 {
			hops = n
		}
	}

	nodes, edges := snap.Neighborhood(node, hops)
	if nodes == nil {
		nodes = []*MapNode{}
	}
	if edges == nil {
		edges = []*MapEdge{}
	}

	resp := struct {
		Nodes []*MapNode `json:"nodes"`
		Edges []*MapEdge `json:"edges"`
	}{Nodes: nodes, Edges: edges}
	writeJSONWithETag(w, r, snap.Gen, "neighborhood", "node="+node+"&hops="+strconv.Itoa(hops), resp)
}

// GET /map/clusters?gen=&depth=2 -> HMAP binary (cluster bubble nodes + cluster
// edges). Falls back to the JSON column object when Accept: application/json.
func (s *HITM) mapClustersHandler(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshotOrAccepted(w)
	if !ok {
		return
	}
	depth := 2
	if d := r.URL.Query().Get("depth"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n >= 0 {
			depth = n
		}
	}

	cols := snap.ClusterColumns(depth)

	gen32 := uint32(snap.Gen)
	etag := etagFor(snap.Gen, "clusters", "depth="+strconv.Itoa(depth))

	if wantsJSON(r) {
		body := encodeHMAPJSON(gen32,
			cols.X, cols.Y, cols.R, cols.Color, cols.NType, cols.Status,
			cols.ESrc, cols.EDst, cols.EColor, cols.EFlags)
		writeWithETag(w, r, "application/json", etag, body)
		return
	}

	body := encodeHMAP(gen32,
		cols.X, cols.Y, cols.R, cols.Color, cols.NType, cols.Status,
		cols.ESrc, cols.EDst, cols.EColor, cols.EFlags)
	writeWithETag(w, r, "application/octet-stream", etag, body)
}

// GET /map/labels?gen=&ids=a,b,c -> JSON {id: {label,path,status,kind,count,sampleFlowId}}.
func (s *HITM) mapLabelsHandler(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshotOrAccepted(w)
	if !ok {
		return
	}
	idsParam := r.URL.Query().Get("ids")
	var ids []string
	if idsParam != "" {
		for _, id := range strings.Split(idsParam, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}

	labels := snap.Labels(ids)
	if labels == nil {
		labels = map[string]LabelInfo{}
	}
	writeJSONWithETag(w, r, snap.Gen, "labels", "ids="+idsParam, labels)
}

// wantsJSON reports whether the client asked for the JSON column fallback. The
// HMAP binary is the default; JSON is only returned when explicitly requested
// via Accept, so a stray Accept: */* still gets the compact binary.
func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}
