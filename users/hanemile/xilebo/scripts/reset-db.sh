#!/usr/bin/env bash
#
# Delete the local database and start fresh.
# Useful during development.
#
# Usage:
#   ./scripts/reset-db.sh                  # delete default xilebo.db
#   ./scripts/reset-db.sh path/to/file.db  # delete specific file
#
set -euo pipefail
cd "$(dirname "$0")/.."

DB="${1:-xilebo.db}"

if [ -f "$DB" ]; then
  rm -f "$DB" "${DB}-wal" "${DB}-shm"
  echo "Deleted $DB"
else
  echo "No database at $DB (nothing to do)"
fi
