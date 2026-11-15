# KScope — Linux Kernel Source Browser

> **Minimal, complete web-based kernel source browser** with kernel/UAPI symbol annotation,
> duplicate structure detection, WebGPU graph visualisation, and a plugin system.

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Quick Start](#quick-start)
4. [REST API Reference](#rest-api-reference)
5. [Symbol Annotation Model](#symbol-annotation-model)
6. [Duplicate Detection](#duplicate-detection)
7. [Graph Renderer (WebGPU)](#graph-renderer-webgpu)
8. [Plugin System](#plugin-system)
9. [Frontend Guide](#frontend-guide)
10. [Building a Plugin](#building-a-plugin)
11. [Configuration](#configuration)
12. [Project Layout](#project-layout)
13. [Known Limitations & Roadmap](#known-limitations--roadmap)

---

## Overview

KScope is a single-binary Go tool that serves a rich browser-based interface for navigating
Linux kernel source code. Its primary purpose is to help developers understand the **boundary
between kernel-internal structures and the stable userspace ABI (UAPI)**, catching places where
the same struct name exists in both namespaces — a common source of subtle compatibility bugs.

Key capabilities:

| Feature | Description |
|---|---|
| **Symbol annotation** | Every symbol is tagged: `kernel`, `uapi`, `exported`, `syscall`, or `duplicated` |
| **Duplicate detection** | Finds structs/typedefs defined in both `include/uapi/` and internal headers |
| **Syntax highlighting** | Regex-based C tokeniser works on incomplete/broken source |
| **Graph explorer** | Force-directed node graph via Canvas2D (WebGPU path when available) |
| **Plugin system** | `.so` plugins extend search, analysis, and introspection |
| **WebSocket status** | Live indexing progress over WS |

---

## Architecture

```
cmd/kscope/main.go
│
├── internal/server/       HTTP server (gorilla/mux), REST API, embedded SPA HTML
│   ├── server.go          Route setup, handlers, WebSocket
│   └── html.go            Embedded single-page application (all JS/CSS inline)
│
├── internal/analyzer/     Symbol indexer and duplicate detector
│   └── analyzer.go        Walk, parse, classify, index, graph payload
│
├── internal/parser/       Syntax highlighter → annotated HTML table
│   └── parser.go          Regex tokeniser + annotation overlay
│
└── internal/plugin/       Plugin loader and built-in plugins
    └── plugin.go          Registry, Plugin interface, builtins
```

**Data flow:**

```
Kernel source tree
       │
       ▼
  Analyzer.Index()           (async goroutine pool, 8 workers)
       │  regex walk → Symbol{Name, Kind, File, Line, Type}
       │  detectDuplicates() → DuplicateReport[]
       ▼
  In-memory index
  ┌─ symbols map[name][]Symbol
  ├─ byFile   map[path][]Symbol
  └─ dups     []DuplicateReport
       │
       ├── /api/search     → Symbol[]
       ├── /api/file       → HighlightResult (HTML + annotations)
       ├── /api/duplicates → DuplicateReport[]
       └── /api/graph      → GraphPayload{Nodes, Edges}
                                    │
                                    ▼
                             Browser: force-directed
                             Canvas2D / WebGPU graph
```

---

## Quick Start

### Prerequisites

- Go 1.22+
- Linux kernel source tree (or any C source directory)

### Build & run

```bash
git clone <repo> && cd kscope
go mod tidy
go build -o kscope ./cmd/kscope

# Point at your kernel tree
./kscope -root /path/to/linux -addr :8080

# Open browser
xdg-open http://localhost:8080
```

### Docker (optional)

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /kscope ./cmd/kscope

FROM alpine:3.19
COPY --from=build /kscope /kscope
ENTRYPOINT ["/kscope"]
CMD ["-root", "/linux", "-addr", ":8080"]
```

---

## REST API Reference

All endpoints are under `/api/`. Responses are JSON.

### `GET /api/status`

Returns indexing state and loaded plugins.

```json
{
  "indexed": true,
  "kernel_root": "/path/to/linux",
  "plugins": [{"name":"uapi-diff","version":"1.0.0","description":"...","actions":["list_duplicates"]}]
}
```

---

### `GET /api/search?q=<query>&kind=<kind>&limit=<n>`

| Parameter | Required | Description |
|---|---|---|
| `q` | yes | Substring search over symbol names |
| `kind` | no | Filter: `kernel`, `uapi`, `duplicated`, `exported`, `syscall` |
| `limit` | no | Max results (default 100) |

```json
{
  "query": "inode",
  "count": 3,
  "results": [
    {"name":"inode","kind":2,"kind_str":"kernel","file":"include/linux/fs.h","line":640,"type":"struct"}
  ]
}
```

---

### `GET /api/symbol/<name>`

Returns all definitions of a symbol (may be >1 if duplicated).

```json
{
  "name": "iovec",
  "count": 2,
  "symbols": [
    {"name":"iovec","kind_str":"kernel","file":"include/linux/uio.h","line":18,"type":"struct","uapi_peer":"include/uapi/linux/uio.h"},
    {"name":"iovec","kind_str":"uapi",  "file":"include/uapi/linux/uio.h","line":14,"type":"struct","uapi_peer":"include/linux/uio.h"}
  ]
}
```

---

### `GET /api/file?path=<relative-path>`

Returns syntax-highlighted HTML for a source file with per-line annotations.

```json
{
  "path": "include/linux/fs.h",
  "html": "<table class=\"src-table\">…</table>",
  "line_symbols": {"640": ["kernel"], "1024": ["duplicated"]},
  "total_lines": 3512,
  "symbols": [{"name":"inode","kind_str":"kernel","line":640,"type":"struct"}]
}
```

---

### `GET /api/tree?path=<relative-dir>`

Lists directory contents (used by the sidebar file tree).

---

### `GET /api/duplicates`

Returns all detected kernel/uapi duplicate structures.

```json
{
  "count": 47,
  "duplicates": [
    {
      "name": "iovec",
      "kernel_file": "include/linux/uio.h",
      "uapi_file":   "include/uapi/linux/uio.h",
      "risk": "high",
      "notes": "Struct 'iovec' defined in both kernel and uapi paths."
    }
  ]
}
```

Risk levels:
- **high** — defined in a `.h` file (interface boundary, easy to confuse)
- **medium** — defined in a `.c` file
- **low** — other cases

---

### `GET /api/graph?max=<n>`

Returns a graph payload for the visualiser (default max 500 nodes).

```json
{
  "nodes": [{"id":"file:include/linux/fs.h","label":"include/linux/fs.h","kind":"file"}],
  "edges": [{"source":0,"target":5,"weight":1},{"source":0,"target":12,"weight":3,"label":"duplicate:iovec"}]
}
```

Duplicate edges have `weight: 3` and a `label` of `"duplicate:<name>"`.

---

### `GET /api/plugins`

Lists all loaded plugins (built-in + .so).

---

### `POST /api/plugin/<name>`

Execute a plugin action.

Request body:
```json
{"action": "diff", "params": {"name":"iovec","kernel_file":"include/linux/uio.h","uapi_file":"include/uapi/linux/uio.h"}}
```

---

### `GET /api/ws` (WebSocket)

Emits JSON status frames every 2 seconds:
```json
{"type":"status","indexed":true}
```

---

## Symbol Annotation Model

KScope classifies every symbol into one of five kinds:

| Kind | Colour | Meaning |
|---|---|---|
| `kernel` | 🟢 Green | Defined outside `include/uapi/` — internal ABI, may change |
| `uapi` | 🟡 Yellow | Defined under `include/uapi/` — stable userspace ABI |
| `exported` | 🟣 Purple | `EXPORT_SYMBOL` / `EXPORT_SYMBOL_GPL` entry |
| `syscall` | 🔵 Blue | `SYSCALL_DEFINE*` or `__x64_sys_*` entry point |
| `duplicated` | 🔴 Red | Same name in both kernel and uapi paths |

Classification is based on path only (no build system required):

```
include/uapi/…          → KindUAPI
include/linux/…         → KindKernel
drivers/…               → KindKernel
EXPORT_SYMBOL(foo)      → KindExported (overrides path)
SYSCALL_DEFINE*(bar)    → KindSyscall (overrides path)
```

---

## Duplicate Detection

After indexing, `detectDuplicates()` scans for struct/enum/typedef names that appear in
**both** a kernel path and a uapi path. Each collision produces a `DuplicateReport`:

```
include/linux/uio.h        struct iovec { ... }   ← kernel-internal copy
include/uapi/linux/uio.h   struct iovec { ... }   ← ABI-stable copy
```

**Why this matters:** Code that accidentally uses the kernel-internal `iovec` when it should
use the uapi version (or vice versa) can cause subtle breakage:

- Kernel may add/reorder fields in the internal copy → layout mismatch
- Passing the wrong struct pointer across the syscall boundary → memory corruption

The Duplicates panel shows all collisions sorted by risk. Clicking a card opens the
kernel-side definition in the source viewer.

---

## Graph Renderer (WebGPU)

The graph panel renders a force-directed layout of the symbol/file relationship graph.

**Node types:**

| Type | Colour | Meaning |
|---|---|---|
| `file` | Blue | Source file |
| `dir` | Grey | Directory |
| `kernel` | Green | Kernel symbol |
| `uapi` | Yellow | UAPI symbol |
| `duplicated` | Red | Duplicated symbol |
| `exported` | Purple | Exported symbol |
| `syscall` | Light blue | Syscall |

**Edges:** file → symbol (weight 1); duplicate pairs get a red edge (weight 3).

**Controls:**
- Drag to pan, scroll to zoom
- Click a file node to open it in the source viewer
- "Show dups only" filters to the duplicate subgraph

**WebGPU:** When `navigator.gpu` is available the context is initialised as a WebGPU
surface. The current implementation uses CPU force simulation with Canvas2D drawing
(portable). Replacing `forceStep()` with a WGSL compute shader for large graphs
(>10k nodes) is the intended extension point.

---

## Plugin System

Plugins are Go shared libraries (`.so`) that export a `Plugin` symbol:

```go
func Plugin() plugin.Plugin { return &myPlugin{} }
```

They implement the `plugin.Plugin` interface:

```go
type Plugin interface {
    Meta()    Metadata
    Execute(q Query) Result
}
```

**Built-in plugins** (always available):

| Name | Actions | Description |
|---|---|---|
| `uapi-diff` | `list_duplicates` | Points to the /api/duplicates endpoint |
| `syscall-map` | `list_syscalls` | Syscall discovery |
| `export-sym` | `list_exports` | EXPORT_SYMBOL enumeration |

**Example plugin** (`plugins/example/struct_diff.go`):

Computes a field-level diff between kernel and uapi versions of a struct.
Build and deploy:

```bash
go build -buildmode=plugin -o plugins/struct-diff.so plugins/example/struct_diff.go
# restart kscope — plugin is auto-loaded
```

---

## Frontend Guide

The SPA is a single embedded HTML string (`internal/server/html.go`). It uses:

- **Vanilla JS** — no framework, no build step, zero dependencies
- **CSS custom properties** — full dark theme, easy to re-skin
- **Canvas2D / WebGPU** — progressive enhancement for the graph
- **Font stack** — JetBrains Mono → Fira Code → Cascadia Code for source

**Panels:**

| Panel | Tab | Description |
|---|---|---|
| Welcome | Home | Quick-reference legend |
| Source | Source | File viewer with annotation sidebar |
| Duplicates | Duplicates | Card grid of kernel/uapi collisions |
| Graph | Graph | Force-directed symbol graph |
| Plugins | Plugins | Loaded plugin cards with action buttons |

**Annotation colours in source view:**

```
Green  background → kernel-internal symbol defined on this line
Yellow background → uapi symbol
Red    background → duplicated symbol (⚠ investigate!)
Purple background → exported symbol
Blue   background → syscall entry point
```

Hovering a line number shows a tooltip with the symbol names.

---

## Building a Plugin

1. Create a new `.go` file (or package) that imports `github.com/kscope/kscope/internal/plugin`.
2. Export a `Plugin` function:

```go
package main

import "github.com/kscope/kscope/internal/plugin"

func Plugin() plugin.Plugin { return &myPlugin{} }

type myPlugin struct{}

func (p *myPlugin) Meta() plugin.Metadata {
    return plugin.Metadata{
        Name:        "my-plugin",
        Version:     "1.0.0",
        Description: "Does something useful",
        Actions:     []string{"action1"},
    }
}

func (p *myPlugin) Execute(q plugin.Query) plugin.Result {
    switch q.Action {
    case "action1":
        return plugin.Result{Data: "hello from plugin"}
    }
    return plugin.Result{Error: "unknown action"}
}
```

3. Build:

```bash
go build -buildmode=plugin -o my-plugin.so my_plugin.go
cp my-plugin.so /path/to/kscope/plugins/
# restart kscope
```

4. Call via the API:

```bash
curl -X POST http://localhost:8080/api/plugin/my-plugin \
  -H 'Content-Type: application/json' \
  -d '{"action":"action1"}'
```

---

## Configuration

| Flag | Default | Description |
|---|---|---|
| `-root` | `.` | Path to kernel source root |
| `-addr` | `:8080` | HTTP listen address |
| `-plugins` | `./plugins` | Directory scanned for .so plugins |
| `-v` | `false` | Verbose logging (includes file:line) |

Environment variables are not used; all configuration is via flags.

---

## Project Layout

```
kscope/
├── cmd/kscope/main.go           Entry point
├── internal/
│   ├── analyzer/analyzer.go     Symbol indexer + duplicate detector
│   ├── parser/parser.go         Syntax highlighter → HTML
│   ├── plugin/plugin.go         Plugin interface + registry + builtins
│   └── server/
│       ├── server.go            HTTP server, REST handlers, WebSocket
│       └── html.go              Embedded SPA (HTML/CSS/JS)
├── plugins/
│   └── example/struct_diff.go  Example plugin (field-level struct diff)
├── go.mod
├── Makefile
└── docs/KSCOPE.md               This file
```

---

## Known Limitations & Roadmap

| Limitation | Notes |
|---|---|
| Regex-based parser | Does not do full AST parsing; handles broken/partial source but may miss complex macro-generated symbols. Integrate `go-tree-sitter` for full C grammar. |
| Single-process index | Index lives in memory; large trees (full kernel) may use ~500MB RAM. A BoltDB/SQLite backend is the natural next step. |
| No cross-reference | Currently does not track which files `#include` which header. Add include-graph edges to the graph renderer. |
| WebGPU compute | Force simulation runs on CPU. A WGSL compute shader would enable 100k+ node graphs at 60fps. |
| No persistent sessions | File and symbol history are not persisted across page reloads. |
| Plugin sandboxing | Plugins run in-process. For untrusted plugins, use a subprocess + JSON-RPC boundary. |

---

*KScope — understand the boundary between kernel and userspace.*
