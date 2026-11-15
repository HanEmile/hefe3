#!/usr/bin/env bash
#
# Run all tests.
#
# Usage:
#   ./scripts/test.sh            # all tests
#   ./scripts/test.sh --verbose  # verbose output
#
set -euo pipefail
cd "$(dirname "$0")/.."

VERBOSE=""
for arg in "$@"; do
  case "$arg" in
    --verbose|-v) VERBOSE="-v" ;;
  esac
done

echo "==> Running Go tests..."
go test $VERBOSE -race -count=1 ./internal/... ./pkg/...

echo ""
echo "==> Running go vet..."
go vet ./cmd/... ./internal/... ./pkg/...

echo ""
echo "All tests passed."
