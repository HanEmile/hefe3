// internal/plugin/plugin.go — Plugin registry, self-describing action schema,
// and built-in plugins. Plugins declare their capabilities via ActionDescriptor
// so the frontend can dynamically build menus without hardcoding anything.
//
// Part of github.com/hanemile/kscope
package plugin

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"plugin"
)

// ── Action schema (self-describing) ──────────────────────────────────────────

// ParamKind describes a single action parameter type.
type ParamKind string

const (
	ParamString  ParamKind = "string"
	ParamInt     ParamKind = "int"
	ParamBool    ParamKind = "bool"
	ParamSymbol  ParamKind = "symbol"  // populated from context
	ParamVersion ParamKind = "version" // populated from context
	ParamFile    ParamKind = "file"
)

// ParamDef describes one parameter for an action.
type ParamDef struct {
	Name     string    `json:"name"`
	Kind     ParamKind `json:"kind"`
	Label    string    `json:"label"`
	Required bool      `json:"required"`
	Default  string    `json:"default,omitempty"`
}

// ActionDescriptor is a fully self-describing plugin action.
// The frontend uses this to build menus and param forms dynamically.
type ActionDescriptor struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	Icon        string     `json:"icon,omitempty"`
	// Which symbol kinds this action applies to (empty = all).
	AppliesTo   []string   `json:"applies_to,omitempty"`
	// Params the UI should collect before calling Execute.
	Params      []ParamDef `json:"params,omitempty"`
	// ResultView hints how to display the result: "json"|"table"|"graph"|"source"
	ResultView  string     `json:"result_view,omitempty"`
}

// Metadata describes a plugin with full action descriptors.
type Metadata struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	Author      string             `json:"author,omitempty"`
	// Actions is the rich self-describing list (supersedes old []string).
	Actions     []ActionDescriptor `json:"actions"`
}

// ActionIDs returns the plain action ID list for backward compat.
func (m Metadata) ActionIDs() []string {
	ids := make([]string, len(m.Actions))
	for i, a := range m.Actions { ids[i] = a.ID }
	return ids
}

// ── Query / Result ────────────────────────────────────────────────────────────

// Query is the request payload passed to a plugin action.
type Query struct {
	Action     string          `json:"action"`
	Params     json.RawMessage `json:"params,omitempty"`
	// Context fields injected by the server — plugins may use these directly.
	Symbol     string          `json:"symbol,omitempty"`
	File       string          `json:"file,omitempty"`
	Line       int             `json:"line,omitempty"`
	Version    string          `json:"version,omitempty"`
	KernelRoot string          `json:"-"`
}

// Result is what a plugin returns.
type Result struct {
	Data       interface{} `json:"data"`
	Error      string      `json:"error,omitempty"`
	// Hint to the frontend on how to render data.
	ResultView string      `json:"result_view,omitempty"`
}

// ── Plugin interface ──────────────────────────────────────────────────────────

// Plugin is the interface every plugin must implement.
type Plugin interface {
	Meta()    Metadata
	Execute(q Query) Result
}

// PluginFactory is the type exported as "Plugin" from a .so file.
type PluginFactory func() Plugin

// ── Registry ──────────────────────────────────────────────────────────────────

// Registry holds all loaded plugins.
type Registry struct {
	plugins []Plugin
}

// NewRegistry scans pluginDir for .so files and prepends built-ins.
func NewRegistry(pluginDir string) *Registry {
	r := &Registry{plugins: BuiltinPlugins()}
	if pluginDir == "" { return r }
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		log.Printf("[plugin] Cannot read plugin dir %s: %v", pluginDir, err)
		return r
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".so" { continue }
		p, err := loadPlugin(filepath.Join(pluginDir, e.Name()))
		if err != nil { log.Printf("[plugin] Failed to load %s: %v", e.Name(), err); continue }
		r.plugins = append(r.plugins, p)
		log.Printf("[plugin] Loaded: %s v%s", p.Meta().Name, p.Meta().Version)
	}
	return r
}

func loadPlugin(path string) (Plugin, error) {
	plug, err := plugin.Open(path)
	if err != nil { return nil, fmt.Errorf("open: %w", err) }
	sym, err := plug.Lookup("Plugin")
	if err != nil { return nil, fmt.Errorf("lookup Plugin symbol: %w", err) }
	factory, ok := sym.(func() Plugin)
	if !ok { return nil, fmt.Errorf("Plugin symbol has wrong type %T", sym) }
	return factory(), nil
}

func (r *Registry) All() []Plugin { return r.plugins }

// Metas returns full metadata including action descriptors.
func (r *Registry) Metas() []Metadata {
	out := make([]Metadata, len(r.plugins))
	for i, p := range r.plugins { out[i] = p.Meta() }
	return out
}

// ActionsForSymbol returns all actions across all plugins that apply to the
// given symbol kind (or have no kind restriction).
func (r *Registry) ActionsForSymbol(kindStr string) []map[string]interface{} {
	var out []map[string]interface{}
	for _, p := range r.plugins {
		m := p.Meta()
		for _, a := range m.Actions {
			applies := len(a.AppliesTo) == 0
			for _, k := range a.AppliesTo {
				if k == kindStr || k == "all" { applies = true; break }
			}
			if applies {
				out = append(out, map[string]interface{}{
					"plugin":      m.Name,
					"plugin_ver":  m.Version,
					"action_id":   a.ID,
					"label":       a.Label,
					"description": a.Description,
					"icon":        a.Icon,
					"params":      a.Params,
					"result_view": a.ResultView,
				})
			}
		}
	}
	return out
}

// Execute dispatches a query to the named plugin.
func (r *Registry) Execute(pluginName string, q Query) Result {
	for _, p := range r.plugins {
		if p.Meta().Name == pluginName { return p.Execute(q) }
	}
	return Result{Error: "plugin not found: " + pluginName}
}
