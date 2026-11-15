#!/usr/bin/env bash
set -euo pipefail

# Test that visibility filtering works correctly across all build tiers.
# Fully dynamic: discovers all objects from source .md files.
#
# Usage: ./scripts/test-visibility.sh [content-dir]

SCRIPTS="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPTS/.." && pwd)"
INPUT="${1:-$ROOT/example}"
SITE="test.local"
PASS=0
FAIL=0

red()   { printf '\033[31mFAIL: %s\033[0m\n' "$1"; FAIL=$((FAIL + 1)); }
green() { printf '\033[32mOK:   %s\033[0m\n' "$1"; PASS=$((PASS + 1)); }

assert() {
    if eval "$2" 2>/dev/null; then green "$1"; else red "$1"; fi
}

echo "=== Visibility Test Suite ==="
echo ""

# ---- Build all three tiers ----
PUBDIR=$(mktemp -d)
INTDIR=$(mktemp -d)
PRIVDIR=$(mktemp -d)
trap "rm -rf $PUBDIR $INTDIR $PRIVDIR" EXIT

echo "Building public..."
"$SCRIPTS/build.sh" "$INPUT" "$PUBDIR" "$SITE" public >/dev/null 2>&1
echo "Building internal..."
"$SCRIPTS/build.sh" "$INPUT" "$INTDIR" "$SITE" internal >/dev/null 2>&1
echo "Building private..."
"$SCRIPTS/build.sh" "$INPUT" "$PRIVDIR" "$SITE" private >/dev/null 2>&1

PUB_COUNT=$(find "$PUBDIR" -name index.html | wc -l | tr -d ' ')
INT_COUNT=$(find "$INTDIR" -name index.html | wc -l | tr -d ' ')
PRIV_COUNT=$(find "$PRIVDIR" -name index.html | wc -l | tr -d ' ')
echo ""
echo "Pages: private=$PRIV_COUNT internal=$INT_COUNT public=$PUB_COUNT"
echo ""

# ---- Collect objects by visibility from source ----
PUBLIC_IDS=""
INTERNAL_IDS=""
PRIVATE_IDS=""

for f in $(find "$INPUT" -name '*.md' -not -path '*/.*' 2>/dev/null); do
    id=$(basename "$f" .md)
    [ "$id" = "README" ] && continue
    vis=$(sed -n 's/^visibility: *//p' "$f" | head -1)
    case "$vis" in
        public)   PUBLIC_IDS="$PUBLIC_IDS $id" ;;
        internal) INTERNAL_IDS="$INTERNAL_IDS $id" ;;
        private)  PRIVATE_IDS="$PRIVATE_IDS $id" ;;
    esac
done

# ---- Test 1: Page existence ----
echo "--- Page Existence ---"
echo ""

for id in $PUBLIC_IDS; do
    assert "$id (public) in public build"   "[ -f '$PUBDIR/$id/index.html' ]"
    assert "$id (public) in internal build"  "[ -f '$INTDIR/$id/index.html' ]"
    assert "$id (public) in private build"   "[ -f '$PRIVDIR/$id/index.html' ]"
done

for id in $INTERNAL_IDS; do
    assert "$id (internal) NOT in public"    "[ ! -f '$PUBDIR/$id/index.html' ]"
    assert "$id (internal) in internal build" "[ -f '$INTDIR/$id/index.html' ]"
    assert "$id (internal) in private build"  "[ -f '$PRIVDIR/$id/index.html' ]"
done

for id in $PRIVATE_IDS; do
    assert "$id (private) NOT in public"     "[ ! -f '$PUBDIR/$id/index.html' ]"
    assert "$id (private) NOT in internal"   "[ ! -f '$INTDIR/$id/index.html' ]"
    assert "$id (private) in private build"   "[ -f '$PRIVDIR/$id/index.html' ]"
done

# ---- Test 2: Search index ----
echo ""
echo "--- Search Index ---"
echo ""

for id in $INTERNAL_IDS $PRIVATE_IDS; do
    assert "$id not in public search.json" \
        "! grep -q '\"id\":\"$id\"' '$PUBDIR/search.json'"
done

for id in $PRIVATE_IDS; do
    assert "$id not in internal search.json" \
        "! grep -q '\"id\":\"$id\"' '$INTDIR/search.json'"
done

# ---- Test 3: No links to invisible objects ----
echo ""
echo "--- Link Leakage (public build) ---"
echo ""

for page in "$PUBDIR"/*/index.html; do
    [ -f "$page" ] || continue
    pname=$(basename "$(dirname "$page")")
    for id in $INTERNAL_IDS $PRIVATE_IDS; do
        assert "public/$pname no link to $id" \
            "! grep -q 'href=\"/$id/\"' '$page'"
    done
done

echo ""
echo "--- Link Leakage (internal build) ---"
echo ""

for page in "$INTDIR"/*/index.html; do
    [ -f "$page" ] || continue
    pname=$(basename "$(dirname "$page")")
    for id in $PRIVATE_IDS; do
        assert "internal/$pname no link to $id" \
            "! grep -q 'href=\"/$id/\"' '$page'"
    done
done

# ---- Test 4: Source .md files don't leak invisible content ----
echo ""
echo "--- Source .md Leakage ---"
echo ""

# Public .md files must not contain vis-start "internal" or "private" content
for mdfile in "$PUBDIR"/*/*.md; do
    [ -f "$mdfile" ] || continue
    pname=$(basename "$(dirname "$mdfile")")
    assert "public/$pname.md no vis-start markers" \
        "! grep -q 'vis-start' '$mdfile'"
    assert "public/$pname.md no vis-end markers" \
        "! grep -q 'vis-end' '$mdfile'"
    # Check that no internal/private object IDs appear as link targets in .md source
    for id in $PRIVATE_IDS; do
        assert "public/$pname.md no ref to private $id" \
            "! grep -q '/$id/' '$mdfile'"
    done
done

# Internal .md files must not contain vis-start "private" content
for mdfile in "$INTDIR"/*/*.md; do
    [ -f "$mdfile" ] || continue
    pname=$(basename "$(dirname "$mdfile")")
    # Should not have private sections — but may have internal markers (those are visible)
    assert "internal/$pname.md no private section content" \
        "! grep -q 'vis-start.*private' '$mdfile'"
    for id in $PRIVATE_IDS; do
        assert "internal/$pname.md no ref to private $id" \
            "! grep -q '/$id/' '$mdfile'"
    done
done

# ---- Test 5: Tier ordering ----
echo ""
echo "--- Tier Sizes ---"
echo ""

assert "private >= internal pages" "[ $PRIV_COUNT -ge $INT_COUNT ]"
assert "internal >= public pages"  "[ $INT_COUNT -ge $PUB_COUNT ]"

# ---- Summary ----
echo ""
echo "==============================="
echo "Passed: $PASS"
echo "Failed: $FAIL"
echo "Total:  $((PASS + FAIL))"
echo "==============================="

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
