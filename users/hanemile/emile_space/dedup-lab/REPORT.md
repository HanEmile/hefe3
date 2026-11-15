# dedup-lab — verification report

Date: 2026-11-13. Tool: `tool/dedup.ml` (411 lines, OCaml 5.5.1 via
`~/.opam`, dune 3.23.1 from nixpkgs, `compiler-libs` from the opam
compiler package — no extra dependencies, no Python fallback needed).

Environment sanity checks done first: `ocamlc -I +compiler-libs
ocamlcommon.cma` hello-world with `Parse.expression`/`Pprintast`;
both copies `dune build` + `dune runtest` clean pre-fold; generator
output is deterministic run-to-run (`diff -r` of two consecutive
baseline runs: empty).

## Note on the copy provenance (deviation from the task description)

The task said `git archive HEAD vokobe2` = pre-human-refactor. In fact
the refactor commit IS `HEAD` (`b7040b9 "vokobe2: factor out duplicated
patterns (-147 lines)"`), and git showed a clean tree at setup time, so:

- `src-head/` = `git archive HEAD~1 vokobe2` (pre-refactor)
- `src-work/` = `cp -r vokobe2` taken immediately after (== `HEAD`,
  post-refactor)

Also: the real `vokobe2` working tree was edited by a concurrent process
*during* the run (Complexity-doc comments; `git status` now shows dirty
files). This lab never wrote outside `dedup-lab/`; both copies are
self-consistent snapshots taken before those edits, so all results below
stand.

## Per-copy results

### src-head (pre-refactor), driver at `--threshold 10`

Dry-run sweep at the spec default `--threshold 25` on all 10 .ml files
(incl. `bin/main.ml`, `test/`): **zero clone classes in every file**
(full log: `verify/src-head/dryrun-t25.log`). Honest landscape: the
real verbatim duplication of this tree tops out at 20 expression-nodes;
the larger duplication the human refactored away was *near*-duplicates
(alpha-varying names), invisible to location-modulo structural equality.

At `--threshold 10` (log: `verify/src-head/driver.log`,
`verify/src-head/logs/`):

| file | folds | detail |
|---|---|---|
| `lib/render.ml` | **1 applied, build OK** | `dedup_shared_1`, 20 nodes, **4 occurrences** across contexts `eval_call; get_page_attr; render_query_`, params `[v ctx]`, lines-saved 9 |
| all others (9 files) | no folds | `DONE no-clone-class-above-threshold` |

Totals: 4109 → 4100 lines (per driver's `cat *.ml`), render.ml
40112 → 39461 bytes, 1083 → 1074 lines. **No reverts.**

The tool's extraction is the human's `local_link`, verbatim:

```ocaml
let dedup_shared_1 v ctx =
let slug = Util.sanitize v in
        match Hashtbl.find_opt ctx.page_index slug with
        | Some _ -> Printf.sprintf {|<a href="/%s" class="local">%s</a>|} slug (Util.html_escape v)
        | None -> Util.html_escape v
```

vs. the human's `local_link ctx v` (identical body, same two free
variables; the human additionally caught a 5th, *alpha-varying* site in
`render_query_`'s tag rendering, using `tag` instead of `v` — correctly
invisible to this tool). One further class was found but skipped by the
negative-line-savings guard (`List [Atom "format"; Atom "list"]`, 10
nodes, would grow the file by 3 lines).

Post-fold: `dune build` OK, `dune runtest` OK
(`verify/src-head/postfold-build.log`).

### src-work (post-refactor), driver at `--threshold 10`

Dry-run at t=25: zero classes in all 10 files. Driver at t=10:
**no folds in any file** — as expected, the human refactor left no
verbatim clones above threshold (the one micro-clone it did leave is
filtered by the negative-savings guard). Totals: 4146 → 4146 lines
(unchanged). Post-fold build/tests trivially OK.

### Behavior preservation proof (both copies)

`run-verify.sh` generated the full site (984 html files, offline mode,
absolute paths into scratch dirs) before and after folding, per copy:

```
verify/src-head/behavior-diff.txt: empty      (diff -r out-baseline out-folded)
verify/src-work/behavior-diff.txt: empty      (diff -r out-baseline out-folded)
```

i.e. `diff -r` empty for **both** copies: every fold kept by the tool
is behavior-preserving, byte-for-byte, on the whole site. (The ~600 MB
of output dirs were deleted after the diff; regenerate with
`run-verify.sh`.)

## Build-gate demonstration (`gate-test/`)

Real runs needed no revert (all kept folds compiled), so a forced case
proves the gate: `gate-test/gatetest.ml` contains a 2-site clone using a
custom operator `( ++ )` defined at top level *after* the first
occurrence. The tool treats operator names as globals, hoists the
clone above the operator definition → `dune build` fails with
`Error: Unbound value (++)` → driver reverts the file byte-identically
and logs:

```
FOLD dedup_shared_1 nodes=84 occurrences=2 contexts=[a; b] params=[n] lines-saved=1
...
REVERTED n-folds=1 reason=dune-build-failed
  build: Error: Unbound value (++)
```

(full log: `logs/gate-test/gatetest.ml.foldlog`; verified the file
contains no `dedup_shared` after the revert.)

## Key numbers

| metric | src-head | src-work |
|---|---|---|
| files scanned | 10 | 10 |
| clone classes @ t=25 (spec default) | 0 | 0 |
| clone classes @ t=10 | 1 (+1 skipped, unproductive) | 0 (+1 skipped, unproductive) |
| folds applied / reverted | 1 / 0 | 0 / 0 |
| project lines before → after | 4109 → 4100 | 4146 → 4146 |
| best fold | 20 nodes, 4 occurrences (human's `local_link`, verbatim) | — |
| post-fold `dune build` | OK | OK |
| post-fold `dune runtest` | OK | OK |
| `diff -r` baseline vs folded site | **empty** | **empty** |

Conclusion: the algorithm works — parse/strip/key/group/fold-at-fixpoint
with textual splicing preserves comments and behavior, and the external
build-gate reverts whatever slips through its own guards. Its reach is
inherently limited to *verbatim* duplication; the interesting residue of
human refactors (generalized near-duplicates) is outside what
structural-equality clone detection can see.
