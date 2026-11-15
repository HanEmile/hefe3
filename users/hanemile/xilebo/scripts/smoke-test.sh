#!/usr/bin/env bash
#
# Run a quick smoke test against a running server.
# Registers a user, creates a room, sends a message, and verifies sync.
#
# Usage:
#   ./scripts/smoke-test.sh              # test against localhost:8080
#   ./scripts/smoke-test.sh :9090        # test against localhost:9090
#
set -euo pipefail

PORT="${1:-:8080}"
PORT="${PORT#:}"
BASE="http://localhost:${PORT}"
API="${BASE}/_xb/client/v1"

fail() { echo "FAIL: $1"; exit 1; }
ok()   { echo "  OK: $1"; }

echo "Smoke testing ${BASE}..."
echo ""

# Well-known
curl -sf "${BASE}/.well-known/xilebo-server" > /dev/null || fail "well-known"
ok "well-known"

# Register
ALICE=$(curl -sf -X POST "${API}/register" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"smoketest_$(date +%s)\",\"password\":\"testpass123\"}") || fail "register"
TOKEN=$(echo "$ALICE" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
HANDLE=$(echo "$ALICE" | python3 -c "import sys,json; print(json.load(sys.stdin)['handle'])")
ok "register ($HANDLE)"

# Create room
ROOM=$(curl -sf -X POST "${API}/rooms" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke-test","topic":"automated test"}') || fail "create room"
ROOM_ID=$(echo "$ROOM" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
ok "create room ($ROOM_ID)"

# Send message
curl -sf -X POST "${API}/rooms/${ROOM_ID}/send" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"smoke test message"}' > /dev/null || fail "send message"
ok "send message"

# Get messages
MSGS=$(curl -sf "${API}/rooms/${ROOM_ID}/messages" \
  -H "Authorization: Bearer $TOKEN") || fail "get messages"
MSG_COUNT=$(echo "$MSGS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))")
[ "$MSG_COUNT" -ge 1 ] || fail "expected at least 1 message, got $MSG_COUNT"
ok "get messages ($MSG_COUNT)"

# Get members
MEMBERS=$(curl -sf "${API}/rooms/${ROOM_ID}/members" \
  -H "Authorization: Bearer $TOKEN") || fail "get members"
ok "get members"

# Sync
SYNC=$(curl -sf "${API}/sync?since=0" \
  -H "Authorization: Bearer $TOKEN") || fail "sync"
ok "sync"

# Profile update
curl -sf -X PUT "${API}/profile" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"display_name":"Smoke Tester","tags":["test"]}' > /dev/null || fail "update profile"
ok "update profile"

# Get profile
PROFILE=$(curl -sf "${API}/profile/${HANDLE}" \
  -H "Authorization: Bearer $TOKEN") || fail "get profile"
ok "get profile"

# Upload keys
curl -sf -X POST "${API}/keys/upload" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"signed_pre_key":{"key_id":1,"public_key":"AQID","signature":"BAUG"},"one_time_keys":[{"key_id":10,"public_key":"BwgJ"}]}' > /dev/null || fail "upload keys"
ok "upload keys"

# ICE servers
curl -sf "${API}/calls/ice-servers" \
  -H "Authorization: Bearer $TOKEN" > /dev/null || fail "ice servers"
ok "ice servers"

echo ""
echo "All smoke tests passed."
