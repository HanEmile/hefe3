#!/usr/bin/env bash
set -exuo pipefail

# Full pipeline: import hefe3, build, and optionally serve.
# Usage: ./scripts/run.sh [visibility] [--serve]
#
# Examples:
#   ./scripts/run.sh                    # private build
#   ./scripts/run.sh public             # public build
#   ./scripts/run.sh private --serve    # build + serve

SCRIPTS="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPTS/.." && pwd)"

VIS="${1:-private}"
SERVE="${2:-}"

INPUT="$ROOT/in"
OUTPUT="$ROOT/out"
SITE="emile.space"
HEFE3="$HOME/src/github.com/hanemile/hefe3"

echo "=== order: full pipeline ==="
echo ""

# Step 1: Import hefe3
if [ -d "$HEFE3" ]; then
  "$SCRIPTS/import.sh" "$HEFE3" "$INPUT"
  # Clean old infra subdirectory if it exists
  rm -rf "$INPUT/infra"
  echo ""
else
  echo "Skipping hefe3 import ($HEFE3 not found)"
  echo ""
fi

# Step 2: Clean output and build fresh
rm -rf "$OUTPUT"
"$SCRIPTS/build.sh" "$INPUT" "$OUTPUT" "$SITE" "$VIS"
echo ""

# Step 3: Serve (optional)
if [ "$SERVE" = "--serve" ]; then
  echo ""
  "$SCRIPTS/serve.sh" "$OUTPUT"
fi
