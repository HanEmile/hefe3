#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_URL="https://downloads.claude.ai/claude-code-releases"
MANIFEST="$SCRIPT_DIR/claude-code-manifest.json"

CURRENT=""
if [[ -f "$MANIFEST" ]]; then
  CURRENT=$(python3 -c "import json; print(json.load(open('$MANIFEST'))['version'])" 2>/dev/null || true)
fi

LATEST=$(curl -fsSL "$BASE_URL/latest")

if [[ "$CURRENT" == "$LATEST" ]]; then
  echo "Already at latest ($LATEST), nothing to do."
  exit 0
fi

echo "Updating claude-code: $CURRENT -> $LATEST"
curl -fsSL "$BASE_URL/$LATEST/manifest.json" -o "$MANIFEST"
echo "Manifest saved to $MANIFEST"
echo ""
echo "Run: nix-build -A ops.darwin.caladan.switch && sudo ./result/bin/switch --show-trace"
