// internal/server/api_symbols.go — Symbol browser and plugin dispatch endpoints.
// GET  /api/symbols            — paginated symbol list for the symbol browser panel
// GET  /api/symbol/{n}/view    — rich symbol detail with xref counts and actions
// GET  /api/plugin-actions     — dynamic action list for a symbol kind
// POST /api/plugin-dispatch    — context-aware plugin execution
//
// Part of github.com/hanemile/kscope
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/hanemile/kscope/internal/plugin"
	"github.com/hanemile/kscope/internal/store"
)


// ── Symbol browser endpoints ──────────────────────────────────────────────────

// handleSymbolView returns a rich "symbol view" for the non-source browser.
// GET /api/symbol/{name}/view?version=
func (s *Server) handleSymbolView(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	vs   := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "version not found", 404); return }
	idx  := vs.Read()
	syms := idx.Lookup(name)
	if len(syms) == 0 { http.Error(w, "symbol not found", 404); return }

	xrefs := idx.XRefsFor(name)
	// Group xrefs by file
	byFile := map[string]int{}
	for _, x := range xrefs { byFile[x.File]++ }

	// Find duplicate peer
	var dupPeer string
	for _, sym := range syms {
		if sym.UAPIPeer != "" { dupPeer = sym.UAPIPeer; break }
	}

	// Available plugin actions for this symbol's kind
	kindStr := "unknown"
	if len(syms) > 0 { kindStr = syms[0].KindStr }
	actions := s.plugins.ActionsForSymbol(kindStr)
	// Also include type-specific actions
	if len(syms) > 0 {
		typeActions := s.plugins.ActionsForSymbol(syms[0].Type)
		seen := map[string]bool{}
		for _, a := range actions { seen[a["action_id"].(string)+"|"+a["plugin"].(string)] = true }
		for _, a := range typeActions {
			k := a["action_id"].(string) + "|" + a["plugin"].(string)
			if !seen[k] { actions = append(actions, a) }
		}
	}

	writeJSON(w, map[string]interface{}{
		"name":        name,
		"definitions": syms,
		"xref_count":  len(xrefs),
		"xref_files":  byFile,
		"dup_peer":    dupPeer,
		"actions":     actions,
		"version":     vs.Label,
	})
}

// handleSymbols returns a paginated list of all symbols (for the symbol browser).
// GET /api/symbols?version=&kind=&type=&q=&page=&per_page=
func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	q      := r.URL.Query()
	vs     := s.vs.Get(q.Get("version"))
	if vs == nil { http.Error(w, "version not found", 404); return }
	idx    := vs.Read()
	kind   := q.Get("kind")
	typ    := q.Get("type")
	search := q.Get("q")
	perPage := 200
	page    := 1
	if p, err := strconv.Atoi(q.Get("page"));     err == nil && p > 0  { page    = p }
	if p, err := strconv.Atoi(q.Get("per_page")); err == nil && p > 0  { perPage = p }

	// Collect all matching symbols (one per name to avoid duplicates in listing)
	type symEntry struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Type     string `json:"type"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		XRefs    int    `json:"xref_count"`
		HasDup   bool   `json:"has_dup"`
	}
	var all []symEntry
	for name, syms := range idx.Symbols {
		if len(syms) == 0 { continue }
		s0 := syms[0]
		if kind   != "" && s0.KindStr != kind { continue }
		if typ    != "" && s0.Type    != typ  { continue }
		if search != "" {
			if !containsFold(name, search) { continue }
		}
		all = append(all, symEntry{
			Name:   name,
			Kind:   s0.KindStr,
			Type:   s0.Type,
			File:   s0.File,
			Line:   s0.Line,
			XRefs:  len(idx.XRefsFor(name)),
			HasDup: s0.UAPIPeer != "",
		})
	}

	// Sort by name
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].Name < all[j-1].Name; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}

	total := len(all)
	start := (page - 1) * perPage
	end   := start + perPage
	if start > total { start = total }
	if end   > total { end   = total }

	writeJSON(w, map[string]interface{}{
		"total":    total,
		"page":     page,
		"per_page": perPage,
		"symbols":  all[start:end],
		"version":  vs.Label,
	})
}

// handlePluginActions returns all actions for a given symbol kind (for dynamic menus).
// GET /api/plugin-actions?kind=kernel&type=struct
func (s *Server) handlePluginActions(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	typ  := r.URL.Query().Get("type")
	acts := s.plugins.ActionsForSymbol(kind)
	if typ != "" && typ != kind {
		extra := s.plugins.ActionsForSymbol(typ)
		seen := map[string]bool{}
		for _, a := range acts { seen[a["action_id"].(string)+"|"+a["plugin"].(string)] = true }
		for _, a := range extra {
			if !seen[a["action_id"].(string)+"|"+a["plugin"].(string)] { acts = append(acts, a) }
		}
	}
	writeJSON(w, map[string]interface{}{"actions": acts, "kind": kind, "type": typ})
}

// handlePluginDispatch dispatches a plugin action with full symbol context.
// POST /api/plugin-dispatch
func (s *Server) handlePluginDispatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plugin  string          `json:"plugin"`
		Action  string          `json:"action"`
		Symbol  string          `json:"symbol"`
		File    string          `json:"file"`
		Line    int             `json:"line"`
		Version string          `json:"version"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), 400); return
	}
	q := plugin.Query{
		Action:  req.Action,
		Params:  req.Params,
		Symbol:  req.Symbol,
		File:    req.File,
		Line:    req.Line,
		Version: req.Version,
	}
	if vs := s.vs.Get(req.Version); vs != nil { q.KernelRoot = vs.Root }

	result := s.plugins.Execute(req.Plugin, q)

	// If result hints at an endpoint, resolve it
	if result.ResultView == "table" || result.ResultView == "graph" {
		if d, ok := result.Data.(map[string]interface{}); ok {
			if ep, ok := d["endpoint"].(string); ok {
				// Append version if missing
				if req.Version != "" && !containsFold(ep, "version=") {
					sep := "?"
					if containsFold(ep, "?") { sep = "&" }
					ep = ep + sep + "version=" + req.Version
				}
				d["resolved_endpoint"] = ep
				result.Data = d
			}
		}
	}

	writeJSON(w, result)
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) &&
		func() bool {
			sl, subl := strings.ToLower(s), strings.ToLower(sub)
			for i := 0; i <= len(sl)-len(subl); i++ {
				if sl[i:i+len(subl)] == subl { return true }
			}
			return false
		}()
}

// ── Call graph ─────────────────────────────────────────────────────────────────

// GET /api/callgraph/{name}?version=&depth=
func (s *Server) handleCallGraph(w http.ResponseWriter, r *http.Request) {
	name  := mux.Vars(r)["name"]
	depth := 2
	if d, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil && d >= 0 { depth = d }
	vs := s.vs.Get(r.URL.Query().Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	writeJSON(w, vs.Read().CallGraph(name, depth))
}

// ── Constants ──────────────────────────────────────────────────────────────────

// GET /api/constants?version=&q=&kind=
func (s *Server) handleConstants(w http.ResponseWriter, r *http.Request) {
	q    := r.URL.Query()
	vs   := s.vs.Get(q.Get("version"))
	if vs == nil { http.Error(w, "not found", 404); return }
	search := q.Get("q")
	kind   := q.Get("kind")
	idx    := vs.Read()
	var out []*store.Constant
	for _, c := range idx.Constants {
		if search != "" && !containsFold(c.Name, search) && !containsFold(c.Value, search) { continue }
		if kind   != "" && c.Kind != kind { continue }
		out = append(out, c)
	}
	// sort by name
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- { out[j], out[j-1] = out[j-1], out[j] }
	}
	writeJSON(w, map[string]interface{}{"count": len(out), "constants": out})
}

// ── Annotations ────────────────────────────────────────────────────────────────

// GET /api/annotations?version=&symbol=&file=&key=
func (s *Server) handleAnnotations(w http.ResponseWriter, r *http.Request) {
	q       := r.URL.Query()
	version := q.Get("version")
	symbol  := q.Get("symbol")
	file    := q.Get("file")
	key     := q.Get("key")
	vs      := s.vs.Get(version)
	if vs == nil { http.Error(w, "not found", 404); return }

	var anns []store.Annotation
	if symbol != "" {
		anns = vs.Annotations.Get(version, symbol)
	} else if file != "" {
		for _, list := range vs.Annotations.ForFile(version, file) {
			anns = append(anns, list...)
		}
	} else {
		anns = vs.Annotations.ListForVersion(version)
	}
	if key != "" {
		var filtered []store.Annotation
		for _, a := range anns { if a.Key == key { filtered = append(filtered, a) } }
		anns = filtered
	}
	writeJSON(w, map[string]interface{}{"annotations": anns, "count": len(anns)})
}

// POST /api/annotations
func (s *Server) handleAddAnnotation(w http.ResponseWriter, r *http.Request) {
	var a store.Annotation
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		http.Error(w, "bad request: "+err.Error(), 400); return
	}
	if a.Version == "" {
		if def := s.vs.Get(""); def != nil { a.Version = def.Label }
	}
	vs := s.vs.Get(a.Version)
	if vs == nil { http.Error(w, "version not found", 404); return }
	a.Source = "user"
	vs.Annotations.Add(a)
	writeJSON(w, a)
}

// DELETE /api/annotations/{id}
func (s *Server) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	// Try all versions
	deleted := false
	s.vs.Each(func(vs *store.VersionStore) {
		if vs.Annotations.Delete(id) { deleted = true }
	})
	writeJSON(w, map[string]interface{}{"deleted": deleted, "id": id})
}

// ── Plugin reload ──────────────────────────────────────────────────────────────

// POST /api/plugins/reload
func (s *Server) handlePluginReload(w http.ResponseWriter, r *http.Request) {
	// Re-scan plugin directory
	newReg := plugin.NewRegistry(s.cfg.PluginDir)
	s.plugins = newReg
	writeJSON(w, map[string]interface{}{
		"plugins": s.plugins.Metas(),
		"count":   len(s.plugins.All()),
	})
}
