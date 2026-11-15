#!/usr/bin/env bash
set -exuo pipefail

# Build the order site.
# Usage: ./scripts/build.sh [input-dir] [output-dir] [site-name] [visibility]

INPUT="${1:-in}"
OUTPUT="${2:-out}"
SITE="${3:-emile.space}"
VIS="${4:-private}"

echo "order build"
echo "  input:      $INPUT"
echo "  output:     $OUTPUT"
echo "  site:       $SITE"
echo "  visibility: $VIS"
echo ""

exec nix-shell --run "sbcl --non-interactive \
  --eval '(require :asdf)' \
  --eval '(push (truename \".\") asdf:*central-registry*)' \
  --eval '(asdf:load-system :order)' \
  --eval '(order:build-site :input-dir \"$INPUT/\" :output-dir \"$OUTPUT/\" :site-name \"$SITE\" :visibility \"$VIS\")'"
