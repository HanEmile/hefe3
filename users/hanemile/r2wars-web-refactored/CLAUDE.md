# r2wars-web — refactor working notes

This is a refactor of the original `r2wars-web` (a Core War-style game where users
submit radare2 assembly "bots" that fight in an emulated arena driven by radare2's
ESIL engine). The original lives on `caladan` at
`~/hefe3/users/hanemile/projects/go/r2wars-web` and a read-only copy is in the
sibling `../r2wars-web/` directory. The refactored tree is here and must be
`scp`'d back to caladan once it builds and tests pass.

## What this app does

- Users register, log in (argon2 password hashing, gorilla/sessions backed by SQLite).
- Users create **bots**: a name + assembly source + one arch (e.g. `x86`) + one bit
  width (8/16/32/64).
- Users create **battles**: an arena (size in bytes, max rounds, allowed archs/bits,
  public flag, a set of owner users).
- Users submit their bots to a battle (validated against the battle's allowed
  arch/bits), then run it. Running assembles each bot with `rasm2`, places them in a
  `malloc://` arena, initialises ESIL, and steps each bot turn-by-turn, capturing a
  transcript (`RawOutput`) shown on the battle page.

## Why the refactor

Original was 4011 lines of flat `package main` in `src/`, with:
- Global mutable `globalState`, no package boundaries.
- `template.ParseGlob(...)` on **every request** (re-reads every template each hit).
- A "deep fetch" pattern using `group_concat` + `strings.Split` to fake joins,
  duplicated ~6× with copy-pasted ID/name parsing.
- Hand-built `INSERT ... VALUES(a,b),(c,d)` SQL string concatenation.
- Buggy `DeleteBattleByID` (references a nonexistent `battleid` column; should be `id`).
- The battle engine inlined in the HTTP handler, **hardcoded to exactly 2 bots**,
  with broken win detection.
- Massive copy-paste in handlers (session fetch, error-redirect, template render).

## New layout

```
cmd/r2wars-web/main.go     entrypoint: flags, wiring, graceful start
internal/model/            domain types (User, Bot, Battle, Arch, Bit) — no deps
internal/store/            SQLite layer: schema, prepared stmts, transactions,
                           real relational queries (no group_concat hacks),
                           gorilla sessions store. Constructor takes a db path.
internal/engine/           battle engine, generalised to N bots; r2 backend behind
                           an interface (R2 + Assembler); a DSL for declaring
                           battles headlessly; structured BattleResult.
internal/web/              HTTP handlers + cached templates + middleware + helpers.
templates/  bots/          unchanged from original.
```

## Design rules

- **Drop-in replacement**: same HTTP routes and same on-disk SQLite schema as the
  original, so it can replace the deployed binary without data migration. (Schema is
  additive-compatible; the original tables/columns are preserved.)
- Templates parsed **once** at startup into a `*template.Template`, cloned/executed
  per request. No `ParseGlob` in handlers.
- All multi-statement DB writes use a single transaction. Relations fetched with
  real `JOIN`s scanned into slices, not `group_concat`+`Split`.
- Engine has **no** direct r2pipe dependency in its core logic: it talks to an
  `R2` interface. Production uses an r2pipe-backed impl; the DSL/round logic is
  unit-testable, and engine integration tests drive **real radare2** (installed:
  r2 6.1.x locally and on caladan, go 1.22+).
- Keep the original's German-tinged casual comment voice where it adds context, but
  drop dead/commented-out code.

## Environment

- `r2`, `rasm2`, `radare2` 6.1.x present locally (nix) and on caladan.
- Run server: `CGO_ENABLED=0 SESSION_KEY=<32 bytes> go run ./cmd/r2wars-web`
  (also needs `SALT` env for password hashing; original used `os.Getenv("SALT")`).
- Tests: `go test ./...` (engine integration tests require r2 on PATH).

## Build & run (note: CGO_ENABLED=0 is REQUIRED)

r2pipe-go's CGO/native path links `libr_core` which isn't present, so everything
must build with `CGO_ENABLED=0` (it then uses the r2 subprocess pipe). The original
`run.sh` already set this.

```
CGO_ENABLED=0 SESSION_KEY=<32b> SALT=<salt> VERSION=<v> go run ./cmd/r2wars-web \
  -databasepath ./main.db -sessiondbpath ./sessions.db -templates ./templates
CGO_ENABLED=0 go test ./...   # engine tests drive real r2; skip if rasm2 absent
```

## Key behaviour changes vs original

- Battle engine generalised from hardcoded 2 bots to **N bots** (round-robin turns).
- `theend` flag reset before each bot's step → correct per-turn death detection.
- DSL (`internal/engine/dsl.go`) to declare battles headlessly; `SpecFromModel`
  builds an engine.Spec from a deep-fetched `model.Battle`.
- Fixed bugs: `DeleteBattle` (wrong column name → now works), `InsertBattle` owner
  link used id 0 (→ uses real new id), register username-uniqueness check inverted.
- Templates parsed once at startup; all link-replace/multi-write ops use transactions;
  relations fetched with real JOINs (no group_concat+Split).
- ESIL trap note: r2 6.1.x doesn't flip the trap hooks for int3/hlt/div0/etc in this
  build, so `theend`-based death rarely triggers in practice; engine is correct when
  r2 does flip it. This matches original behaviour.

## Status

- [x] skeleton, go.mod, model package
- [x] internal/store (10 tests pass)
- [x] internal/engine (+ DSL) (11 tests pass, real r2)
- [x] internal/web (5 handler tests pass)
- [x] cmd/r2wars-web wiring
- [x] builds + tests pass locally (`CGO_ENABLED=0 go test ./...`)
- [x] end-to-end smoke test: register→login→2 bots→battle→submit→run produced a
      correct N-bot transcript via real radare2
- [ ] scp back to caladan
