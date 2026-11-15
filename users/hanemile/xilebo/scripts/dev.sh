#!/usr/bin/env bash
#
# Start the xilebo server and web frontend for local development.
# The Go server runs on :8080, the frontend dev server on :3000.
#
# Usage:
#   ./scripts/dev.sh              # start both
#   ./scripts/dev.sh --server     # server only
#   ./scripts/dev.sh --web        # frontend only
#
set -euo pipefail
cd "$(dirname "$0")/.."

cleanup() {
  echo ""
  echo "Shutting down..."
  kill $SERVER_PID 2>/dev/null || true
  wait 2>/dev/null
}
trap cleanup EXIT

# Copy config if it doesn't exist
if [ ! -f config.toml ]; then
  cp config.example.toml config.toml
  echo "Created config.toml from config.example.toml"
fi

echo "Building..."
mkdir -p bin
go build -o bin/xilebo-server ./cmd/xilebo-server
go build -o bin/xilebo-tui ./cmd/xilebo-tui

echo "Starting server on :8080..."
./bin/xilebo-server -config config.toml &
SERVER_PID=$!

echo ""
echo "========================================="
echo "  Web:  http://localhost:8080"
echo "  API:  http://localhost:8080/_xb/client/v1"
echo "  TUI:  ./bin/xilebo-tui"
echo "========================================="
echo ""
echo "The web frontend is served directly — no build step."
echo "Press Ctrl+C to stop."

wait
