#!/usr/bin/env bash
# run-verify.sh -- full verification protocol for one source copy:
#   baseline build+run -> fold (driver with build-gate) -> rebuild+test
#   -> re-run -> diff -r baseline vs folded output (must be empty).
#
# usage: run-verify.sh <name> <copy-dir> [threshold]
# env:   INPUT_DIR (default: <lab>/../input)
# Run inside nix-shell shell.nix.
set -uo pipefail

NAME=${1:?} COPY=${2:?} THRESH=${3:-10}
LAB=$(cd "$(dirname "$0")" && pwd)
COPY=$(cd "$COPY" && pwd)
INPUT=${INPUT_DIR:-"$LAB/../input"}
V="$LAB/verify/$NAME"
OUT1="$V/out-baseline" OUT2="$V/out-folded"
rm -rf "$V"; mkdir -p "$V"

step() { echo "### $*"; }

step "1/6 baseline build + test"
(cd "$COPY" && dune build && dune runtest) > "$V/baseline-build.log" 2>&1 \
  || { echo "BASELINE BUILD FAILED (see $V/baseline-build.log)"; exit 1; }

step "2/6 baseline generation -> $OUT1"
(cd "$COPY" && dune exec bin/main.exe -- "$INPUT" "$OUT1" emile.space --offline) \
  > "$V/baseline-run.log" 2>&1 || { echo "BASELINE RUN FAILED"; exit 1; }

step "3/6 dry-run sweep at spec default threshold=25 (honest landscape)"
for f in "$COPY"/lib/*.ml "$COPY"/bin/main.ml; do
  "$LAB/tool/_build/default/dedup.exe" "$f" --threshold 25 --dry-run
done > "$V/dryrun-t25.log" 2>&1

step "4/6 driver (fold + per-file build gate) at threshold=$THRESH"
LOGDIR="$V/logs" bash "$LAB/driver.sh" "$COPY" "$THRESH" > "$V/driver.log" 2>&1 \
  || { echo "DRIVER FAILED (see $V/driver.log)"; exit 1; }

step "5/6 rebuild + runtest on folded copy"
(cd "$COPY" && dune build) > "$V/postfold-build.log" 2>&1 \
  || { echo "POST-FOLD BUILD FAILED"; exit 1; }
cd "$COPY" && dune runtest >> "$V/postfold-build.log" 2>&1 \
  && echo "POST-FOLD TESTS: ok" || echo "POST-FOLD TESTS: failed (see log)"

step "6/6 re-run folded generator -> $OUT2, diff -r"
(cd "$COPY" && dune exec bin/main.exe -- "$INPUT" "$OUT2" emile.space --offline) \
  > "$V/folded-run.log" 2>&1 || { echo "FOLDED RUN FAILED"; exit 1; }
if diff -r "$OUT1" "$OUT2" > "$V/behavior-diff.txt" 2>&1; then
  echo "BEHAVIOR-PRESERVED: diff -r empty (${NAME})"
  echo "empty" > "$V/behavior-diff.txt"
else
  echo "BEHAVIOR CHANGED! see $V/behavior-diff.txt"
fi
