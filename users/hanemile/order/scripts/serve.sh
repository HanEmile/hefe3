#!/usr/bin/env bash
set -euo pipefail

# Serve the built site locally.
# Usage: ./scripts/serve.sh [output-dir] [port]

OUTPUT="${1:-public-site}"
PORT="${2:-8000}"

echo "Serving $OUTPUT on http://localhost:$PORT"
exec nix-shell -p python3 --run "python3 -m http.server -d '$OUTPUT' $PORT"
