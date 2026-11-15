# KScope Architecture

**KScope** is a Linux kernel source browser focused on the boundary between
kernel-internal symbols and the userspace ABI (UAPI). This document describes
the full architecture for contributors.

---

## Table of Contents

1. [High-Level Overview](#high-level-overview)
2. [Directory Layout](#directory-layout)
3. [Data Flow](#data-flow)
4. [Package Reference](#package-reference)
5. [Frontend Architecture (BSP)](#frontend-architecture-bsp)
6. [Plugin System](#plugin-system)
7. [Annotation System](#annotation-system)
8. [Symbol Classification](#symbol-classification)
9. [Adding a New Feature](#adding-a-new-feature)
10. [Known Quirks](#known-quirks)

---

## High-Level Overview

```
┌──────────────────────────────────────────────────────────┐
│  Browser (SPA)                                           │
│  BSP panel tree │ symbol browser │ graph │ node editor   │
└──────────┬───────────────────────────────────────────────┘
           │  HTTP + SSE  /api/*
┌──────────┴───────────────────────────────────────────────┐
│  Go HTTP Server  (internal/server/)                      │
│  api.go · api_symbols.go · git.go · sse.go               │
└──────┬────────────────────────────┬──────────────────────┘
       │                            │
┌──────┴──────────┐    ┌────────────┴────────────┐
│  Worker Pool    │    │  Version Registry        │
│  internal/worker│    │  map[label]*VersionStore │
└──────┬──────────┘    └────────────┬────────────┘
       │ jobs                       │ atomic swap
┌──────┴──────────┐    ┌────────────┴────────────┐
│  Indexer        │    │  Store (Index snapshot)  │
│  internal/indexer│   │  internal/store/         │
└─────────────────┘    └─────────────────────────┘
```

All inter-component messages use `internal/protocol.Msg` — a typed envelope
that can be sent over Go channels today and over a remote transport tomorrow.

---

## Directory Layout

```
kscope/
├── cmd/kscope/main.go          Entry point, CLI flags
├── docs/ARCHITECTURE.md        This file
├── internal/
│   ├── analyzer/               Compat shim (logic moved to store+indexer)
│   ├── indexer/
│   │   ├── indexer.go          Source walker → symbol/xref/callgraph extraction
│   │   └── annotate.go         copy_from_user detection, struct-size analysis
│   ├── parser/
│   │   └── parser.go           C syntax highlighter → annotated HTML
│   ├── plugin/
│   │   ├── plugin.go           Registry, ActionDescriptor, Query/Result types
│   │   └── builtins.go         Built-in plugins (xref-trace, uapi-diff, struct-layout…)
│   ├── protocol/
│   │   └── message.go          Wire protocol: Msg{Kind, Payload} for all tasks
│   ├── server/
│   │   ├── server.go           HTTP server wiring, route setup
│   │   ├── api.go              Core source/search/tree/diff/graph handlers
│   │   ├── api_symbols.go      Symbol browser, annotations, call graph handlers
│   │   ├── embed.go            //go:embed web/ → buildHTML() assembler
│   │   ├── sse.go              SSE hub, version registry helpers
│   │   ├── git.go              git tag/checkout handlers
│   │   └── web/                Static assets (embedded at build time)
│   │       ├── index.html      SPA shell — <script src="js/..."> tags
│   │       ├── style.css       Full stylesheet (CSS variables, no frameworks)
│   │       └── js/
│   │           ├── core.js     State, Bus, SSE, workspaces, BSP engine
│   │           ├── panels.js   Source/diff/dup/xref panel logic
│   │           ├── graph.js    Force-directed graph (fault-tolerant)
│   │           ├── callgraph.js Function call graph panel
│   │           ├── nodeed.js   Canvas node editor
│   │           ├── symbols.js  Symbol browser panel
│   │           ├── structview.js Struct layout visualizer
│   │           ├── popup.js    Universal "send to plugin" popup
│   │           └── bsp.js      Binary Space Partitioning panel system
│   ├── store/
│   │   ├── index.go            Immutable Index snapshot + query helpers
│   │   └── annotation.go       Per-symbol per-version arbitrary annotations
│   └── worker/
│       └── pool.go             Generic priority job queue
└── plugins/
    └── example/struct_diff.go  Example .so plugin
```

---

## Data Flow

### Indexing

```
1. server.New() → pool.Submit(KindIndexStart, payload{root, label})
2. Worker goroutine picks up job
3. indexer.Handler() runs:
   a. WalkDir → count files (progress denominator, async)
   b. Per-file goroutines (8 workers):
      - parseFile() → []Symbol, []XRef, []CallEdge, []Constant
      - detectAnnotations() → copy_from_user sites, struct field analysis
   c. detectDuplicates() → []DuplicateReport
   d. store.NewIndex populated
4. swap(newIndex) atomically replaces VersionStore.idx
5. Indexer emits KindIndexProgress messages → SSE hub → browser
6. On KindIndexDone, browser refreshes tree + symbol panel
```

### HTTP Request (source file)

```
GET /api/file?path=mm/hmm.c&version=v6.9

server/api.go handleFile()
  → vs = versionRegistry.Get("v6.9")
  → idx = vs.Read()                    ← lock-free snapshot
  → syms = idx.FileSymbols(clean)
  → annMap = buildAnnotations(syms)    ← merge store annotations
  → parser.Highlight(full, annMap)     ← HTML with <span class="hl-*">
  → JSON response {html, symbols, line_symbols}
```

### SSE Event Stream

```
browser EventSource /api/events
  ← event: index.progress  {label, phase, files, total, pct, symbols}
  ← event: job.running     {job_id, kind, state, progress}
  ← event: job.done        {job_id}
  ← : keepalive            (every 20s)
```

### Plugin Dispatch

```
POST /api/plugin-dispatch
  body: {plugin, action, symbol, file, line, version, params}

server/api_symbols.go handlePluginDispatch()
  → plugin.Registry.Execute(pluginName, Query{...})
  → Plugin.Execute(q) → Result{Data, ResultView}
  → if ResultView=="graph": data.endpoint resolved with version
  → JSON response → browser popup renders per ResultView
```

---

## Package Reference

### `internal/store`

**`Index`** — immutable snapshot, atomically swapped after indexing:
- `Symbols  map[string][]*Symbol`   — by name, may have kernel+uapi entries
- `ByFile   map[string][]*Symbol`   — by relative path
- `XRefs    map[string][]*XRef`     — by symbol name, usages in .c bodies
- `CallEdges map[string][]*CallEdge` — by caller function name
- `Constants map[string][]*Constant` — `#define` values with context
- `Dups     []*DuplicateReport`

**`VersionStore`** — one per indexed git checkout:
- `Swap(*Index)` — atomic replace (write lock, then release)
- `Read() *Index` — returns pointer to current snapshot (read lock)
- `Annotations` — live, mutable, stored separately (see annotation.go)

**`annotation.go`** — `AnnotationStore` holds `map[version][name][]Annotation`.
Annotations survive re-indexing because they are stored outside the Index.

### `internal/indexer`

`Handler(swap)` returns a `worker.HandlerFunc`. The swap callback is the only
way data flows back to the server — the indexer has no reference to the server.

`parseFile(fullPath, relPath)` returns `([]Symbol, []XRef, []CallEdge, []Constant)`.
It is pure: no side effects, no locks. Goroutines merge results under a mutex.

### `internal/parser`

`Highlight(path, annotations)` reads the file, tokenises with layered regex
(comment > string > macro > keyword > type > number > function), and produces
an HTML `<table>` where each row is a source line. Known symbols get
`.sym-link` spans injected by the frontend (not here) to keep the parser
stateless with respect to the symbol index.

**Multiline comment handling:** `reBlkC` (`(?s)/\*.*?\*/`) matches across
newlines. It runs before all other passes so block comment content is never
re-tokenised as keywords.

### `internal/worker`

A fixed-size goroutine pool. Jobs carry a `Priority` (0=low, 5=normal, 10=high).
The queue is a buffered channel; ordering is FIFO within a priority class.
Every state transition emits a `protocol.Msg` via the `SetEmit` hook —
the server wires this directly to the SSE hub.

**Remote worker path:** Replace `SetEmit` with a function that serialises
`Msg` to JSON and POST it to a remote worker URL. The remote worker runs the
same `Handler` functions and POSTs results back. The `protocol.Msg.CorrelID`
links responses to requests.

### `internal/protocol`

`Msg` is the universal envelope:
```go
type Msg struct {
    ID       string          // unique, time-based
    Kind     Kind            // "index.progress", "job.done", etc.
    TS       time.Time
    Priority Priority
    CorrelID string          // reply links to request
    Source   string          // originating component
    Target   string          // destination ("" = any worker)
    Payload  json.RawMessage // type-specific payload
    Error    string
}
```

Every `Kind` constant has a corresponding payload struct in the same file.
New message kinds: add the constant + payload struct, register a handler in
the worker pool.

---

## Frontend Architecture (BSP)

The UI uses a **Binary Space Partitioning tree** (`bsp.js`). Every node is
either a `split` (horizontal or vertical, with a ratio) or a `leaf` (a panel
slot). This replaces the fixed CSS grid.

```
BSPNode {
  type: 'split' | 'leaf'
  // split:
  dir: 'h' | 'v'
  ratio: 0.5        ← drag the divider to change
  a: BSPNode
  b: BSPNode
  // leaf:
  panel: Panel      ← {view, state}
}
```

**Workspace** = one BSP tree root. Tabs switch between workspaces.

**Panel** = `{view: 'source'|'symbols'|'graph'|…, state: {}}`. The view
name determines which JS module renders it.

**Resizing:** Dividers are draggable. `bsp.layoutTo(el, node)` recursively
positions children using `position:absolute` with pixel coordinates calculated
from the container size and stored ratios.

**Adding a new panel type:**
1. Add `{id:'mypanel', label:'My Panel', icon:'X'}` to `VIEWS` in `core.js`
2. Add a `case 'mypanel':` in `viewTemplate()` returning the HTML skeleton
3. Add a `case 'mypanel':` in `initSlotView()` calling your init function
4. Add a JS module (`web/js/mypanel.js`) and a `<script src>` in `index.html`

---

## Plugin System

Plugins are Go shared libraries (`.so`) or built-in structs. Both implement:

```go
type Plugin interface {
    Meta()    Metadata      // name, version, self-describing actions
    Execute(q Query) Result // stateless execution
}
```

**`ActionDescriptor`** makes the system self-extending:
```go
type ActionDescriptor struct {
    ID          string      // machine name, stable across versions
    Label       string      // shown in menus
    Icon        string      // emoji
    AppliesTo   []string    // symbol kinds; empty = all
    Params      []ParamDef  // auto-filled: symbol/version; prompted: others
    ResultView  string      // "json"|"table"|"graph"|"source"|"struct"
}
```

When a user right-clicks a symbol, the browser fetches
`GET /api/plugin-actions?kind=kernel&type=struct`. The server calls
`Registry.ActionsForSymbol(kind)` which iterates all loaded plugins and
returns every matching `ActionDescriptor`. The popup renders them dynamically.
**No frontend changes are needed when a new plugin is loaded.**

**Writing a plugin:**
```go
// myplugin/main.go
package main
import "github.com/hanemile/kscope/internal/plugin"

func Plugin() plugin.Plugin { return &myPlugin{} }
type myPlugin struct{}
func (p *myPlugin) Meta() plugin.Metadata {
    return plugin.Metadata{
        Name: "my-plugin", Version: "1.0.0",
        Actions: []plugin.ActionDescriptor{{
            ID: "my_action", Label: "Do something", Icon: "★",
            AppliesTo: []string{"struct"},
            Params: []plugin.ParamDef{
                {Name:"symbol", Kind:plugin.ParamSymbol, Required:true},
            },
            ResultView: "json",
        }},
    }
}
func (p *myPlugin) Execute(q plugin.Query) plugin.Result {
    // q.Symbol, q.File, q.Line, q.Version are pre-filled
    return plugin.Result{Data: map[string]any{"found": q.Symbol}}
}
```

Build: `go build -buildmode=plugin -o myplugin.so myplugin/main.go`

---

## Annotation System

Annotations live in `store.AnnotationStore`, which is **separate from the
index** so they survive re-indexing. They are keyed by `(version, symbolName)`.

```go
type Annotation struct {
    ID        string          // uuid
    Version   string          // kernel version label
    Symbol    string          // symbol name
    Key       string          // e.g. "size_model", "copy_from_user_site"
    Value     string          // arbitrary string value
    ValueType string          // "string"|"int"|"bool"|"json"
    Note      string          // human-readable explanation
    Source    string          // "auto"|"plugin:name"|"user"
    CreatedAt time.Time
}
```

**Automatic annotations** are created by the indexer:
- `copy_from_user_site` — lines containing `copy_from_user()`
- `sizeof_call` — lines containing `sizeof(struct_name)` for a known struct

**Plugin annotations** are created via `POST /api/annotations`.

**User annotations** are created via the UI (right-click → "Annotate").

**Cross-version viewing:** When viewing version B, annotations from version A
are shown as ghost overlays if the symbol exists in B too.

---

## Symbol Classification

```
Path contains /uapi/  →  KindUAPI    (yellow) — stable ABI
EXPORT_SYMBOL(*)      →  KindExported (purple)
SYSCALL_DEFINE*(*)    →  KindSyscall  (cyan)
Kernel + UAPI peer    →  KindDuplicated (red) ← primary analysis target
Otherwise             →  KindKernel   (green)
```

Within function bodies (`.c` files), struct member accesses are checked:
if the struct's definition is in a uapi path, the usage is tagged
`uapi-usage`; if in a kernel-internal path, `kernel-usage`. This lets
the source view highlight uapi vs kernel struct uses differently
**within the same function**.

---

## Adding a New Feature

### New API endpoint
1. Add handler `func (s *Server) handleFoo(w, r)` in `api.go` or a new `api_foo.go`
2. Register route in `server.go setupRoutes()`
3. Add `api('/foo')` call in the relevant JS module
4. Document in this file

### New panel view
1. Add to `VIEWS` in `core.js`
2. Add `case 'foo':` in `viewTemplate()` and `initSlotView()`
3. Create `web/js/foo.js` with an `initFooSlot(slot, st)` function
4. Add `<script src="js/foo.js"></script>` in `index.html`
5. Add `/*INJECT_JS_FOO*/` placeholder if using injection mode

### New symbol kind
1. Add constant to `store/index.go` `SymbolKind` iota
2. Update `String()` method
3. Add CSS class `.kb-newkind` in `style.css`
4. Add color to `kindColorCSS()` in `graph.js`
5. Update `classifyPath()` or add detection regex in `indexer.go`

### New annotation key
1. Define a constant in `store/annotation.go`
2. Add detection in `indexer/annotate.go`
3. Add rendering in `parser.go` if it affects source view
4. Add icon/color in `style.css`

---

## Known Quirks

**Regex-based parsing:** The indexer uses regexp, not a full AST. This means:
- Macros that expand to struct definitions are not tracked
- `typeof()` usages are not resolved
- Template-like macros (e.g. `LIST_HEAD_INIT`) are not expanded
- Fix: integrate `go-tree-sitter` with the C grammar for accurate AST walking

**Index memory usage:** A full kernel tree (~30k .c/.h files) produces
~500MB RAM index. The `ByFile` map dominates. Mitigation: add a `bolt.db`
on-disk backing store; the in-memory map becomes an LRU cache.

**XRef noise:** The xref extractor matches any identifier in function bodies
against known symbol names. Short or common names (e.g. `len`, `buf`) will
have many false-positive xrefs. Filter: skip names shorter than 5 chars,
or maintain a C keyword + type blacklist.

**BSP drag on mobile:** The divider drag uses `mousemove`. Add `touchmove`
handlers for tablet use.

**Plugin hot-reload:** On Linux, `plugin.Open()` uses `dlopen()` which
caches the library handle. A hot-reload must dlclose and re-dlopen, which
Go's `plugin` package does not support. Workaround: restart the server,
or use subprocess + JSON-RPC for plugin isolation.

**Go module path:** The module is `github.com/hanemile/kscope`. All imports
must use this path. The example plugin in `plugins/example/` must be built
with the same Go toolchain version as the server binary.
