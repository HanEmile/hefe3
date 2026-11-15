# dedup-lab — AST-based automatic code deduplication for OCaml

An experiment: can a dumb mechanical tool rediscover the duplication a
human refactored out of `vokobe2` (commit `b7040b9`, -147 lines) —
and fold it back out, provably without changing program behavior?

Pure OCaml, `compiler-libs` only. No compiler-libs fallback to Python
was needed.

## Layout

```
tool/          the dedup tool (OCaml, dune project, compiler-libs)
driver.sh      folds every .ml of a copied project, dune-build safety
               gate per file, reverts+logs folds that break the build
run-verify.sh  end-to-end protocol on one source copy:
               baseline build/run -> dry-run sweep -> driver -> rebuild
               + runtest -> re-run -> diff -r of generated sites
src-head/      vokobe2 copy at HEAD~1 (pre-human-refactor; clones live)
src-work/      vokobe2 copy of the post-refactor tree (expect ~none)
gate-test/     toy dune project proving the driver's build-gate revert
verify/        per-copy artifacts: logs, fold logs, behavior diff
logs/          instrument fold logs (gate-test)
```

## Build & run

Everything runs inside the repo's `shell.nix` (opam switch OCaml 5.5.1,
nixpkgs dune; `compiler-libs` ships with the opam compiler, so no extra
package is needed — verified with a hello-world use of
`Parse`/`Pprintast`):

```sh
nix-shell shell.nix --run 'cd dedup-lab/tool && dune build'
# the binary:
#   dedup-lab/tool/_build/default/dedup.exe

# analyze/fold one file in place:
dedup-lab/tool/_build/default/dedup.exe <file.ml> \
    [--threshold N] [--max-iters N] [--same-context-ok] [--dry-run]

# fold a whole project copy with the build gate:
nix-shell shell.nix --run 'bash dedup-lab/driver.sh dedup-lab/src-head/vokobe2 10'

# full verification protocol (rebuilds copies' outputs, ~600 MB disk):
nix-shell shell.nix --run 'bash dedup-lab/run-verify.sh src-head dedup-lab/src-head/vokobe2 10'
```

CLI flags:

- `--threshold N` — minimum expression-node count of a candidate subtree
  (default 25, per spec). On vokobe2, **all verbatim clones are below
  25 nodes**, so the default honestly finds nothing; `--threshold 10`
  is where the real clone class of the pre-refactor tree shows up.
- `--max-iters N` — fixpoint cap (default 10).
- `--same-context-ok` — also fold clone classes whose occurrences all
  live in one top-level binding (default: occurrences must span >= 2
  distinct top-level contexts, per spec).
- `--dry-run` — log only, don't rewrite the file.
- `DEDUP_DEBUG=1` — also dump the final folded text to stdout.

## Design notes ("extract-method by AST clone detection, at fixpoint")

1. **Parse** with `Parse.implementation` over `Lexing.from_string`, so
   every location carries absolute byte offsets (`pos_cnum`).
2. **Normalize for comparison**: an `Ast_mapper` whose `location` field
   returns `Location.none` everywhere; the normalized subtree is keyed
   by `Marshal.to_string` — location-independent structural equality
   (attributes, which can carry behavior, stay in the key).
3. **Enumerate** every expression subtree with an `Ast_iterator`:
   node count (expression nodes, self included) >= threshold, skip
   literals/identifiers/no-arg constructors, and — important — skip
   **ghost locations** (parser desugar artifacts like the synthetic
   `fun` under `let f x = ...`; their spans don't bracket valid source
   text, and splicing them produced `p =` bodies in an early bug).
   Each occurrence records its span, its enclosing top-level structure
   item index/start offset, and a *context* (the name of the enclosing
   top-level binding, or `#itemN`).
4. **Clone classes**: group by key; prune occurrences nested inside a
   same-class occurrence (keep the outer); require >= 2 surviving
   occurrences spanning >= 2 distinct contexts (unless
   `--same-context-ok`). One class per iteration is folded: the best
   `nodes × occurrences` score. Fixpoint via re-parse each iteration.
5. **Folding** (all textual splices, computed from one parse):
   - free vars = unqualified `Pexp_ident` used minus `Ppat_var`/
     `Ppat_alias` bound inside the subtree (first-use order);
   - a free var is a *global* iff it's bound by a top-level item
     strictly **before** the earliest occurrence's item (so hoisting
     cannot hit not-yet-defined names; operator-ish names and dotted
     longidents are treated as globals);
   - the rest become parameters of `let dedup_shared_K p1 .. pn = <body>`
     where `<body>` is the representative occurrence's verbatim source
     span, inserted immediately before that enclosing structure item;
   - every occurrence's span is replaced by `(dedup_shared_K p1 .. pn)`;
   - comments and formatting outside the spliced spans survive
     byte-for-byte because the file is never reprinted (Pprintast would
     drop all comments).
6. **Cheap guards in-tool**: a folded text that fails to re-parse is
   rejected immediately (class skipped); folds with negative line
   savings (the new `let` + call overhead exceeds the removed lines)
   are skipped and the class is blacklisted for the rest of the run.
   (Without the latter, the tool happily “deduplicates” 1-line sexp
   literals at a net +3 lines.)
7. **Build-gate** (`driver.sh`): per file, after the tool ran and
   produced >= 1 fold, run `dune build` in the copy; on failure restore
   the pre-fold bytes and record `REVERTED` + compiler stderr in the
   fold log. See `verify/gate-test/` for a forced demonstration
   (a clone using a user operator bound *later* at top level hoists to
   an unbound identifier; the file is reverted byte-identically).
8. **Behavior proof** (`run-verify.sh`): baseline and folded copies
   each build the whole site into scratch dirs (`--offline`, absolute
   paths, never touching the repo's `output/`); `diff -r` must be empty.

## Known limitations

- **Verbatim clones only.** Equality is modulo locations/whitespace,
  not modulo identifier renaming. Most of the human refactor was
  *generalizing* near-duplicates (the three disk caches, gen_index_pages
  skeletons, RDP-style repetition): those differ in names/values and are
  invisible to this tool. `--threshold 25` on both copies finds zero
  classes; the one noteworthy verbatim class (the 4x `local_link`
  pattern, 20 nodes) needs a lower threshold.
- **Same-module only, per file.** No cross-file folding (a shared
  extraction in `Util` would need multi-file coordination).
- Free-var analysis is lexical and conservative: a name bound inside
  the subtree *anywhere* is considered bound (could mask a use outside
  that binder's scope in exotic shadowing); record labels and
  type-directed disambiguation are not tracked — a folded hover can
  fail to typecheck (→ build gate reverts).
- Evaluation-order preservation relies on the extraction being a plain
  `let f args = body` with the call placed at the original site;
  effectful bodies stay at the same control point, so this is sound,
  but it is only *argued*, not proven.
- Node-count metric counts expression nodes only (patterns/types not
  counted); threshold semantics is therefore “how big is the code”, not
  “how many lines”.
- Iteration cap 10 by default; each iteration re-parses the whole file
  (fine at ~1500 lines/file).

## Results

See [REPORT.md](REPORT.md). Headline: at threshold 10 the tool
re-discovers the human's `local_link` extraction in the pre-refactor
tree **verbatim** (same body, same two parameters, 4 occurrences), finds
nothing above any threshold in the post-refactor copy, and the generated
site is byte-identical before/after folding in both copies
(`diff -r` empty), with the build gate demonstrably reverting a bad fold
in a forced test.
