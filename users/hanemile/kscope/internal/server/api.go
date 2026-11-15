// internal/server/api.go — HTTP API handlers: status, search, file, diff,
// duplicates, graph, xref, jobs, plugins, tree. Version-aware via ?version=.
//
// Part of github.com/hanemile/kscope
package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/hanemile/kscope/internal/parser"
	"github.com/hanemile/kscope/internal/plugin"
	"github.com/hanemile/kscope/internal/protocol"
	"github.com/hanemile/kscope/internal/store"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"versions": s.vs.List(),
		"pool":     s.pool.Stats(),
		"plugins":  s.plugins.Metas(),
		"git_root": s.cfg.KernelRoot,
		"workers":  s.cfg.Workers,
	})
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"versions": s.vs.List()})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"jobs": s.pool.Jobs()})
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	writeJSON(w, map[string]interface{}{"cancelled": s.pool.Cancel(id), "id": id})
}

func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "version not found", 404); return }
	writeJSON(w, map[string]interface{}{"job_id": s.enqueueIndex(vs.Root, vs.Label, vs.GitTag), "label": vs.Label})
}

// handleIndexDir enqueues indexing of a specific subdirectory (e.g. "drivers").
// The results are merged into the existing VersionStore index.
// POST /api/index-dir  body: {"sub_dir":"drivers","version":"linux"}
func (s *Server) handleIndexDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SubDir  string `json:"sub_dir"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SubDir == "" {
		http.Error(w, "sub_dir required", 400); return
	}
	// Validate path — must be a clean relative directory name, no traversal
	clean := filepath.Clean(req.SubDir)
	if clean == "." || strings.Contains(clean, "..") || filepath.IsAbs(clean) {
		http.Error(w, "invalid sub_dir", 400); return
	}
	vs := s.vs.Get(req.Version)
	if vs == nil { http.Error(w, "version not found", 404); return }

	// Check the directory actually exists
	fullPath := filepath.Join(vs.Root, clean)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "directory not found: "+clean, 404); return
	}

	label := vs.Label + "+" + clean // e.g. "linux+drivers"
	jobID := s.pool.Submit(
		protocol.KindIndexStart, protocol.PriorityNormal,
		protocol.IndexStartPayload{
			Root:    vs.Root,
			Label:   vs.Label, // merge into same VersionStore
			SubDir:  clean,
			Workers: s.cfg.Workers,
		},
		"index-dir:"+clean,
	)
	writeJSON(w, map[string]interface{}{
		"job_id":  jobID,
		"label":   label,
		"sub_dir": clean,
		"root":    vs.Root,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 { limit = l }
	vs := s.vs.Get(q.Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	idx := vs.Read()
	results := idx.Search(q.Get("q"), limit*4)
	if kind := q.Get("kind"); kind != "" {
		var f []*store.Symbol
		for _, sym := range results { if sym.KindStr == kind { f = append(f, sym); if len(f) >= limit { break } } }
		results = f
	} else if len(results) > limit { results = results[:limit] }
	writeJSON(w, map[string]interface{}{"query": q.Get("q"), "count": len(results), "results": results})
}

func (s *Server) handleSymbol(w http.ResponseWriter, r *http.Request) {
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	name := mux.Vars(r)["name"]
	idx  := vs.Read()
	syms := idx.Lookup(name)
	if len(syms) == 0 { http.Error(w, "not found", 404); return }
	writeJSON(w, map[string]interface{}{
		"name": name, "count": len(syms), "symbols": syms,
		"xref_count": len(idx.XRefsFor(name)),
	})
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" { http.Error(w, "path required", 400); return }
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") { http.Error(w, "forbidden", 403); return }
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	idx  := vs.Read()
	syms := idx.FileSymbols(clean)
	annMap := buildAnnotations(syms)
	res, err := parser.Highlight(filepath.Join(vs.Root, clean), annMap)
	if err != nil { http.Error(w, err.Error(), 500); return }

	type symEnriched struct {
		*store.Symbol
		XRefCount int `json:"xref_count"`
	}
	enriched := make([]symEnriched, len(syms))
	for i, sym := range syms { enriched[i] = symEnriched{sym, len(idx.XRefsFor(sym.Name))} }

	writeJSON(w, map[string]interface{}{
		"path": clean, "version": vs.Label,
		"html": res.HTML, "line_symbols": res.LineSymbols,
		"total_lines": res.TotalLines, "symbols": enriched,
	})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := filepath.Clean(q.Get("path"))
	if strings.HasPrefix(path, "..") { http.Error(w, "forbidden", 403); return }
	vsA := s.vs.Get(q.Get("a")); vsB := s.vs.Get(q.Get("b"))
	if vsA == nil || vsB == nil { http.Error(w, "version not found", 404); return }
	side := func(vs *store.VersionStore) map[string]interface{} {
		syms := vs.Read().FileSymbols(path)
		res, err := parser.Highlight(filepath.Join(vs.Root, path), buildAnnotations(syms))
		if err != nil { return map[string]interface{}{"version": vs.Label, "error": err.Error()} }
		return map[string]interface{}{"version": vs.Label, "html": res.HTML, "total_lines": res.TotalLines, "symbols": syms}
	}
	writeJSON(w, map[string]interface{}{"path": path, "a": side(vsA), "b": side(vsB)})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path"); if rel == "" { rel = "." }
	clean := filepath.Clean(rel)
	if strings.HasPrefix(clean, "..") { http.Error(w, "forbidden", 403); return }
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }

	entries, err := os.ReadDir(filepath.Join(vs.Root, clean))
	if err != nil { http.Error(w, err.Error(), 500); return }
	type E struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}
	var out []E
	for _, e := range entries {
		info, _ := e.Info(); sz := int64(0)
		if info != nil && !e.IsDir() { sz = info.Size() }
		p := filepath.Join(clean, e.Name()); if clean == "." { p = e.Name() }
		out = append(out, E{e.Name(), p, e.IsDir(), sz})
	}
	writeJSON(w, map[string]interface{}{"path": clean, "entries": out, "version": vs.Label})
}

func (s *Server) handleDuplicates(w http.ResponseWriter, r *http.Request) {
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	dups := vs.Read().Dups
	writeJSON(w, map[string]interface{}{"count": len(dups), "duplicates": dups})
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	max := 500
	if m, err := strconv.Atoi(r.URL.Query().Get("max")); err == nil && m > 0 { max = m }
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	writeJSON(w, vs.Read().GraphData(max))
}

func (s *Server) handleXRef(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	limit := 300
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil { limit = l }
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	refs := vs.Read().XRefsFor(name)
	if len(refs) > limit { refs = refs[:limit] }
	writeJSON(w, map[string]interface{}{"symbol": name, "count": len(refs), "xrefs": refs})
}

func (s *Server) handleXRefGraph(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	depth := 1
	if d, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil { depth = d }
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	writeJSON(w, vs.Read().XRefGraphData(name, depth))
}

func (s *Server) handlePluginList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"plugins": s.plugins.Metas()})
}

func (s *Server) handlePluginExec(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var q plugin.Query
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil { http.Error(w, "bad request", 400); return }
	if vs := s.vs.Get(""); vs != nil { q.KernelRoot = vs.Root }
	writeJSON(w, s.plugins.Execute(name, q))
}

func buildAnnotations(syms []*store.Symbol) map[int][]parser.Annotation {
	m := make(map[int][]parser.Annotation)
	for _, sym := range syms {
		m[sym.Line] = append(m[sym.Line], parser.Annotation{Line: sym.Line, Kind: sym.KindStr, Name: sym.Name})
	}
	return m
}
