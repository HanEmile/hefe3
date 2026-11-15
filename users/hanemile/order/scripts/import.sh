#!/usr/bin/env bash
set -euo pipefail

# Import infrastructure data from hefe3 into the order content directory.
# Usage: ./scripts/import.sh [hefe3-path] [content-dir]

HEFE3="${1:-$HOME/src/github.com/hanemile/hefe3}"
CONTENT="${2:-example}"

if [ ! -d "$HEFE3" ]; then
  echo "error: hefe3 not found at $HEFE3"
  exit 1
fi

echo "Importing hefe3 from $HEFE3 -> $CONTENT/"
python3 "$(dirname "$0")/import-hefe3.py" "$HEFE3" "$CONTENT"
