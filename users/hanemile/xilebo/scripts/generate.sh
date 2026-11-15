#!/usr/bin/env bash
#
# Regenerate code from SQL schemas.
# Run this after modifying sql/queries/*.sql or sql/schema/*.sql.
#
# Requires: sqlc (go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest)
#
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v sqlc &>/dev/null; then
  echo "Installing sqlc..."
  go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
fi

echo "==> Generating SQL code..."
sqlc generate
echo "    internal/storage/sqlite/queries/"

echo ""
echo "Done."
