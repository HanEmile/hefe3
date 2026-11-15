#!/usr/bin/env bash
#
# Build the xilebo server binary and frontend bundle for production.
#
# Usage:
#   ./scripts/build.sh
#
# Output:
#   bin/xilebo-server    — server binary
#   web/dist/            — frontend bundle
#
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Building server..."
mkdir -p bin
go build -trimpath -o bin/xilebo-server ./cmd/xilebo-server
echo "    bin/xilebo-server"

echo "==> Building TUI client..."
go build -trimpath -o bin/xilebo-tui ./cmd/xilebo-tui
echo "    bin/xilebo-tui"

echo ""
echo "The web frontend is pure JS — no build step needed."
echo "It is served directly from web/ by the server."
echo ""
echo "Done. Run with:"
echo "  ./bin/xilebo-server               # start server (web at http://localhost:8080)"
echo "  ./bin/xilebo-tui                   # terminal client"
echo "  ./bin/xilebo-tui -server URL       # connect to a specific server"
