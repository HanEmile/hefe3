// internal/plugin/builtins.go — Built-in plugin implementations.
// Each plugin is fully self-describing via ActionDescriptor so the
// frontend builds menus dynamically without any hardcoded knowledge.
//
// Part of github.com/hanemile/kscope
package plugin

// BuiltinPlugins returns the always-available built-in plugin set.
func BuiltinPlugins() []Plugin {
	return []Plugin{
		&symbolInfoPlugin{},
		&xrefTracePlugin{},
		&uapiDiffPlugin{},
		&syscallMapPlugin{},
		&exportSymPlugin{},
	}
}

// ── symbol-info ───────────────────────────────────────────────────────────────

type symbolInfoPlugin struct{}

func (p *symbolInfoPlugin) Meta() Metadata {
	return Metadata{
		Name: "symbol-info", Version: "1.0.0", Author: "builtin",
		Description: "Displays full symbol information: kind, location, duplicates, peers",
		Actions: []ActionDescriptor{
			{
				ID: "show_info", Label: "Show symbol info", Icon: "ℹ",
				Description: "Display kind, file, line, duplicate peer, and xref count",
				AppliesTo:   []string{"all"},
				Params:      []ParamDef{
					{Name: "symbol",  Kind: ParamSymbol,  Label: "Symbol",  Required: true},
					{Name: "version", Kind: ParamVersion, Label: "Version", Required: false},
				},
				ResultView: "json",
			},
			{
				ID: "show_definition", Label: "Go to definition", Icon: "→",
				Description: "Navigate to the source definition of the symbol",
				AppliesTo:   []string{"all"},
				Params:      []ParamDef{
					{Name: "symbol", Kind: ParamSymbol, Label: "Symbol", Required: true},
				},
				ResultView: "source",
			},
		},
	}
}

func (p *symbolInfoPlugin) Execute(q Query) Result {
	switch q.Action {
	case "show_info":
		return Result{
			Data:       map[string]interface{}{"symbol": q.Symbol, "version": q.Version, "endpoint": "/api/symbol/" + q.Symbol},
			ResultView: "json",
		}
	case "show_definition":
		return Result{
			Data:       map[string]interface{}{"symbol": q.Symbol, "action": "navigate", "endpoint": "/api/symbol/" + q.Symbol},
			ResultView: "source",
		}
	}
	return Result{Error: "unknown action: " + q.Action}
}

// ── xref-trace ────────────────────────────────────────────────────────────────

type xrefTracePlugin struct{}

func (p *xrefTracePlugin) Meta() Metadata {
	return Metadata{
		Name: "xref-trace", Version: "1.0.0", Author: "builtin",
		Description: "Traces call chains and symbol usages across the kernel tree",
		Actions: []ActionDescriptor{
			{
				ID: "trace_usages", Label: "Trace usages", Icon: "🔍",
				Description: "Find all places this symbol is used",
				AppliesTo:   []string{"all"},
				Params: []ParamDef{
					{Name: "symbol",  Kind: ParamSymbol,  Label: "Symbol",        Required: true},
					{Name: "depth",   Kind: ParamInt,     Label: "Depth",         Required: false, Default: "1"},
					{Name: "version", Kind: ParamVersion, Label: "Version",       Required: false},
				},
				ResultView: "table",
			},
			{
				ID: "trace_callers", Label: "Trace callers", Icon: "↑",
				Description: "Find functions that call this symbol",
				AppliesTo:   []string{"func", "function"},
				Params: []ParamDef{
					{Name: "symbol",  Kind: ParamSymbol,  Label: "Symbol",  Required: true},
					{Name: "version", Kind: ParamVersion, Label: "Version", Required: false},
				},
				ResultView: "table",
			},
			{
				ID: "xref_graph", Label: "XRef graph", Icon: "◎",
				Description: "Build a visual graph of symbol usages",
				AppliesTo:   []string{"all"},
				Params: []ParamDef{
					{Name: "symbol", Kind: ParamSymbol, Label: "Symbol",      Required: true},
					{Name: "depth",  Kind: ParamInt,    Label: "Graph depth", Required: false, Default: "1"},
				},
				ResultView: "graph",
			},
		},
	}
}

func (p *xrefTracePlugin) Execute(q Query) Result {
	switch q.Action {
	case "trace_usages", "trace_callers":
		return Result{Data: map[string]interface{}{"symbol": q.Symbol, "endpoint": "/api/xref/" + q.Symbol}, ResultView: "table"}
	case "xref_graph":
		return Result{Data: map[string]interface{}{"symbol": q.Symbol, "endpoint": "/api/xrefgraph/" + q.Symbol}, ResultView: "graph"}
	}
	return Result{Error: "unknown action: " + q.Action}
}

// ── uapi-diff ─────────────────────────────────────────────────────────────────

type uapiDiffPlugin struct{}

func (p *uapiDiffPlugin) Meta() Metadata {
	return Metadata{
		Name: "uapi-diff", Version: "1.0.0", Author: "builtin",
		Description: "Detects structures defined in both kernel and uapi/ paths",
		Actions: []ActionDescriptor{
			{
				ID: "list_duplicates", Label: "List all duplicates", Icon: "⑂",
				Description: "All struct names existing in both kernel and uapi paths",
				ResultView:  "table",
			},
			{
				ID: "check_symbol", Label: "Check for duplicate", Icon: "⑂",
				Description: "Check if this symbol has a kernel/uapi duplicate",
				AppliesTo:   []string{"struct", "enum", "typedef"},
				Params:      []ParamDef{{Name: "symbol", Kind: ParamSymbol, Label: "Symbol", Required: true}},
				ResultView:  "json",
			},
		},
	}
}

func (p *uapiDiffPlugin) Execute(q Query) Result {
	switch q.Action {
	case "list_duplicates":
		return Result{Data: map[string]interface{}{"endpoint": "/api/duplicates"}, ResultView: "table"}
	case "check_symbol":
		return Result{Data: map[string]interface{}{"symbol": q.Symbol, "endpoint": "/api/symbol/" + q.Symbol}, ResultView: "json"}
	}
	return Result{Error: "unknown action: " + q.Action}
}

// ── syscall-map ───────────────────────────────────────────────────────────────

type syscallMapPlugin struct{}

func (p *syscallMapPlugin) Meta() Metadata {
	return Metadata{
		Name: "syscall-map", Version: "1.0.0", Author: "builtin",
		Description: "Maps SYSCALL_DEFINE* entries to their argument structures",
		Actions: []ActionDescriptor{
			{
				ID: "list_syscalls", Label: "List syscalls", Icon: "⚡",
				Description: "Enumerate all SYSCALL_DEFINE* entries", ResultView: "table",
			},
			{
				ID: "show_args", Label: "Show syscall args", Icon: "⚡",
				Description: "Show argument structures for a syscall",
				AppliesTo:   []string{"syscall"},
				Params:      []ParamDef{{Name: "symbol", Kind: ParamSymbol, Label: "Syscall", Required: true}},
				ResultView:  "source",
			},
		},
	}
}

func (p *syscallMapPlugin) Execute(q Query) Result {
	return Result{Data: map[string]interface{}{"endpoint": "/api/search?kind=syscall", "symbol": q.Symbol}, ResultView: "table"}
}

// ── export-sym ────────────────────────────────────────────────────────────────

type exportSymPlugin struct{}

func (p *exportSymPlugin) Meta() Metadata {
	return Metadata{
		Name: "export-sym", Version: "1.0.0", Author: "builtin",
		Description: "Enumerates EXPORT_SYMBOL and EXPORT_SYMBOL_GPL entries",
		Actions: []ActionDescriptor{
			{
				ID: "list_exports", Label: "List exported symbols", Icon: "📤",
				Description: "All EXPORT_SYMBOL / EXPORT_SYMBOL_GPL entries", ResultView: "table",
			},
			{
				ID: "check_export", Label: "Check if exported", Icon: "📤",
				Description: "Check whether this symbol is exported to modules",
				AppliesTo:   []string{"func", "exported"},
				Params:      []ParamDef{{Name: "symbol", Kind: ParamSymbol, Label: "Symbol", Required: true}},
				ResultView:  "json",
			},
		},
	}
}

func (p *exportSymPlugin) Execute(q Query) Result {
	return Result{Data: map[string]interface{}{"endpoint": "/api/search?kind=exported", "symbol": q.Symbol}, ResultView: "table"}
}
