#!/usr/bin/env bash
# driver.sh -- fixpoint-fold every .ml file of a copied vokobe2 tree,
# with a dune-build safety gate per file: if a fold breaks the build,
# the file is reverted to its pre-fold text and the revert is logged.
#
# usage:  driver.sh <project-copy-dir> [threshold] [max-iters]
# env:    LOGDIR  (default: <lab>/logs/<basename of copy>)
#
# Run inside `nix-shell shell.nix` (needs ocaml + dune on PATH).
set -uo pipefail

COPY=${1:?usage: driver.sh <project-copy-dir> [threshold] [max-iters]}
THRESH=${2:-25}
MAXI=${3:-10}

LAB=$(cd "$(dirname "$0")" && pwd)
COPY=$(cd "$COPY" && pwd)

# build the tool (idempotent)
(cd "$LAB/tool" && dune build) || { echo "tool build failed"; exit 1; }
TOOL="$LAB/tool/_build/default/dedup.exe"

LOGDIR=${LOGDIR:-"$LAB/logs/$(basename "$COPY")"}
mkdir -p "$LOGDIR"

BACKUP=$(mktemp -d "${TMPDIR:-/tmp}/dedup-backup.XXXXXX")

files=$(find "$COPY" -name '*.ml' -not -path '*/_build/*' | sort)

# snapshot originals
for f in $files; do
  rel=${f#"$COPY"/}
  mkdir -p "$BACKUP/$(dirname "$rel")"
  cp "$f" "$BACKUP/$rel"
done

total_folds=0
total_reverted=0
before=$(cat $files | wc -l)

for f in $files; do
  rel=${f#"$COPY"/}
  log="$LOGDIR/${rel//\//_}.foldlog"
  "$TOOL" "$f" --threshold "$THRESH" --max-iters "$MAXI" > "$log" 2>&1
  n=$(grep -c '^FOLD ' "$log" || true)
  if [ "$n" = "0" ]; then
    echo "== $rel: no folds"
    continue
  fi
  total_folds=$((total_folds + n))
  if (cd "$COPY" && dune build) > "$log.build" 2>&1; then
    echo "== $rel: $n fold(s), build OK"
    grep -E '^(FOLD|RESULT)' "$log"
    rm -f "$log.build"
  else
    cp "$BACKUP/$rel" "$f"
    total_reverted=$((total_reverted + 1))
    {
      echo "REVERTED n-folds=$n reason=dune-build-failed"
      sed 's/^/  build: /' "$log.build" | head -20
    } >> "$log"
    rm -f "$log.build"
    echo "== $rel: $n fold(s) REVERTED (build failed, see $log)"
  fi
done

after=$(cat $files | wc -l)
echo
echo "SUMMARY copy=$COPY threshold=$THRESH" | tee -a "$LOGDIR/SUMMARY.txt"
echo "SUMMARY folds-applied=$total_folds incl-reverted=$total_reverted" | tee -a "$LOGDIR/SUMMARY.txt"
printf "SUMMARY lines-before=%s lines-after=%s\n" "$before" "$after" | tee -a "$LOGDIR/SUMMARY.txt"
echo "logs: $LOGDIR"
rm -rf "$BACKUP"
