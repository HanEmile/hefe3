# taint — source-level data-flow tracer for manual security analysis

Traces tainted data from user-controlled input sources to a target site,
annotating every step with its taint level and the branch conditions that
must hold to reach it.

## Supported languages

| Extension         | Language |
|-------------------|----------|
| `.c` `.h`         | C        |
| `.cpp` `.cc` `.hpp` | C++    |
| `.rs`             | Rust     |
| `.go`             | Go       |
| `.py`             | Python   |

## Build

```bash
cargo build --release
# binary: target/release/taint
```

## Usage

```
taint [OPTIONS] --source <FILE>... --target <TARGET>

Options:
  -s, --source <FILE>...          Source file(s) to analyse
  -t, --target <TARGET>           What to trace to (see below)
      --extra-sources <FN,...>    Extra taint sources (default list + these)
      --sanitisers <FN,...>       Functions that strip taint (e.g. strndup,validate)
      --depth <N>                 Max backward search depth (default: 32)
      --json                      Emit JSON instead of coloured text
      --all-paths                 Include paths with no taint at the target
```

### Target formats

| Format                  | Meaning                                              |
|-------------------------|------------------------------------------------------|
| `function_name`         | Any tainted use inside the named function            |
| `function_name:variable`| A specific variable inside a named function          |
| `line:col`              | Exact source location (1-based line and column)      |

## Examples

```bash
# Trace all taint reaching the `process_input` function
taint -s src/parser.c -t process_input

# Trace the specific variable `key` inside `decrypt`
taint -s crypto.c crypto_utils.c -t decrypt:key

# Trace to an exact line in a Python file
taint -s server.py -t 87:12

# Multi-file Rust project, custom sources, sanitiser
taint -s src/main.rs src/net.rs \
      --extra-sources read_packet,parse_header \
      --sanitisers validate_length,check_bounds \
      -t handle_request:payload

# Machine-readable JSON output for a solver / downstream tool
taint -s vuln.c -t vuln_func --json > paths.json
```

## Taint levels

| Level     | Meaning                                                        |
|-----------|----------------------------------------------------------------|
| `FULL`    | All data originates from a user-controlled source              |
| `PARTIAL` | Derived from a mix of tainted and untainted data (arithmetic,  |
|           | struct field access, etc.)                                     |
| `NONE`    | No user-controlled data reaches this node                      |

## Built-in taint sources

```
C/C++/POSIX : read fread fgets gets scanf fscanf sscanf
              recv recvfrom recvmsg accept
              getenv getline getdelim mmap
Rust        : read_line read_to_string read_to_end from_utf8_unchecked
Go          : Read ReadFrom ReadString ReadBytes Scan Scanln
Python      : input raw_input read readline readlines recv recvfrom
Generic     : request body query form json sys.stdin
```

Add project-specific sources with `--extra-sources`.

## Output format (text)

```
════════════════════════════════════════════════════════════════════════
 TAINT REPORT   target: process_input:buf   (2 path(s) found)
════════════════════════════════════════════════════════════════════════

┌─  path #0 — sink taint: FULL
├─  SOURCE  [FULL]  net.c:14:5    buf ← recv(fd, buf, sizeof(buf), 0)  ← USER INPUT
│         ↳  if fd > 0 [⚠ TAINTED]
├─  ASSIGN  [FULL]  net.c:17:5    trimmed = strip(buf) [buf]
│         ↳  if trimmed != NULL
└─  SINK    [FULL]  parser.c:42:3 process_input(trimmed)
└
  2 fully-tainted path(s)  0 partially-tainted path(s)
```

Each path node shows:
- **Kind**: SOURCE / ASSIGN / CALL / BRANCH / ENTRY / SINK
- **Taint**: FULL / PART / NONE badge
- **Location**: file:line:col
- **Description**: the expression / statement text
- **Conditions**: branch predicates (with taint annotation) that must hold to reach this node

## JSON output schema

```json
[
  {
    "index": 0,
    "sink_taint": "Full",
    "nodes": [
      {
        "kind": "Source",
        "loc": { "file": "net.c", "line": 14, "col": 5 },
        "description": "buf ← recv(fd, buf, 1024, 0)  ← USER INPUT",
        "taint": "Full",
        "conditions": ["fd > 0 [⚠ TAINTED]"]
      },
      ...
    ]
  }
]
```

## Architecture

```
source file
    │
    ▼
tree-sitter parse  (language-specific grammar, unified Node API)
    │
    ▼
Extractor  —  walks AST, extracts:
    • Function  (name, params, body)
    • VarDef    (name, rhs expression, referenced variables)
    • CallSite  (callee, args, return variable)
    • Branch    (condition expression, referenced variables)
    │
    ▼
TaintEngine  —  fixed-point forward propagation
    • Seeds: built-in + extra sources → TaintLevel::Full
    • Sanitisers → TaintLevel::None
    • Arithmetic mixing → TaintLevel::Partial
    │
    ▼
PathFinder  —  backward slice from target
    • Resolves sink nodes matching --target
    • DFS backward through def-use chains
    • Collects branch conditions per path node
    │
    ▼
render_text / render_json
```

## Limitations

- This is a **linear** (intra-procedural) analysis with lightweight cross-call
  linkage via call site return variables. It does not perform full
  inter-procedural pointer analysis.
- Aliasing is not tracked (two pointers to the same buffer are treated
  independently).
- The AST extraction uses heuristic node-kind matching across tree-sitter
  grammars; complex macro expansions or metaprogramming may be missed.
- The backward search is bounded by `--depth` to avoid infinite loops in
  recursive code.

These limitations make it suited for **manual** triage and confirmation, not
automated proof. Use the output as a starting point for a human reviewer or
as input to a more precise solver.
