#!/usr/bin/env bash
# Phase 8 end-to-end test: Project + API Key management + auth on all endpoints.
#
# Prerequisites:
#   - Server running on BASE_URL (default http://localhost:8080)
#   - DB migrated (includes 005_api_keys.sql)
#   - O3 configured (or just DB mode)
#
# Usage:
#   ./scripts/test_phase8_e2e.sh
#   BASE_URL=http://localhost:8080 ./scripts/test_phase8_e2e.sh
set -e

BASE="${BASE_URL:-http://localhost:8080}"
PASS=0
FAIL=0

green() { printf "\033[32m✓ %s\033[0m\n" "$1"; ((PASS++)) || true; }
red()   { printf "\033[31m✗ %s\033[0m\n" "$1"; ((FAIL++)) || true; }

assert_status() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    green "$label (HTTP $actual)"
  else
    red "$label — expected HTTP $expected, got $actual"
  fi
}

echo "═══════════════════════════════════════════════"
echo " AkaveLog Phase 8 — End-to-end test"
echo " Server: $BASE"
echo "═══════════════════════════════════════════════"
echo ""

# ── 1. Unauthenticated push should fail ──────────────────────────────────────
echo "── 1. Push without API key → 401 ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/akavelog/api/v1/push" \
  -H "Content-Type: application/json" \
  -d '{"streams":[{"stream":{"job":"test"},"values":[["1000000000","hello"]]}]}')
assert_status "Push without key" "401" "$STATUS"

# ── 2. Create a project ────────────────────────────────────────────────────────
echo ""
echo "── 2. Create project ──"
CREATE_RESP=$(curl -s -X POST "$BASE/projects" \
  -H "Content-Type: application/json" \
  -d '{"name":"e2e-test-project","owner_email":"ci@test.com"}')
echo "$CREATE_RESP" | head -c 300
echo ""

API_KEY=$(echo "$CREATE_RESP" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
PROJECT_ID=$(echo "$CREATE_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -z "$API_KEY" ]; then
  red "Could not extract API key from response"
  exit 1
fi
if [ -z "$PROJECT_ID" ]; then
  red "Could not extract project_id"
  exit 1
fi
green "Project created: $PROJECT_ID"
green "API Key obtained: ${API_KEY:0:20}..."

# ── 3. Authenticated push ──────────────────────────────────────────────────────
echo ""
echo "── 3. Push with valid API key → 204 ──"
TS_NS=$(python3 -c "import time; print(int(time.time()*1e9))" 2>/dev/null || echo "1700000000000000000")
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/akavelog/api/v1/push" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d "{\"streams\":[{\"stream\":{\"job\":\"e2e-test\",\"level\":\"info\"},\"values\":[[\"$TS_NS\",\"INFO hello from phase8 e2e\"]]}]}")
assert_status "Authenticated push" "204" "$STATUS"

# ── 4. Push with invalid key ───────────────────────────────────────────────────
echo ""
echo "── 4. Push with invalid key → 401 ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/akavelog/api/v1/push" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: akal_thisisnotavalidkey000" \
  -d '{"streams":[{"stream":{"job":"test"},"values":[["1","line"]]}]}')
assert_status "Push with bad key" "401" "$STATUS"

# ── 5. Query without key → 401 ────────────────────────────────────────────────
echo ""
echo "── 5. Query without key → 401 ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/query" \
  -H "Content-Type: application/json" \
  -d '{}')
assert_status "Query without key" "401" "$STATUS"

# ── 6. Query with valid key → 200 ─────────────────────────────────────────────
echo ""
echo "── 6. Query with valid key → 200 ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/query" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{"limit":10}')
assert_status "Query with valid key" "200" "$STATUS"

# ── 7. List projects ───────────────────────────────────────────────────────────
echo ""
echo "── 7. List projects ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/projects")
assert_status "List projects" "200" "$STATUS"

# ── 8. List API keys ───────────────────────────────────────────────────────────
echo ""
echo "── 8. List API keys for project ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/projects/$PROJECT_ID/api-keys")
assert_status "List API keys" "200" "$STATUS"

# ── 9. Create additional API key ───────────────────────────────────────────────
echo ""
echo "── 9. Create additional API key ──"
NEW_KEY_RESP=$(curl -s -X POST "$BASE/projects/$PROJECT_ID/api-keys" \
  -H "Content-Type: application/json" \
  -d '{"name":"extra-key"}')
NEW_KEY=$(echo "$NEW_KEY_RESP" | grep -o '"key":"[^"]*"' | cut -d'"' -f4)
if [ -n "$NEW_KEY" ]; then
  green "Additional key created: ${NEW_KEY:0:20}..."
else
  red "Failed to create additional key"
fi

# ── 10. Revoke original key ────────────────────────────────────────────────────
echo ""
echo "── 10. Revoke original API key ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X DELETE "$BASE/projects/$PROJECT_ID/api-keys/$API_KEY")
assert_status "Revoke key" "204" "$STATUS"

# ── 11. Revoked key can no longer push ────────────────────────────────────────
echo ""
echo "── 11. Push with revoked key → 401 ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/akavelog/api/v1/push" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{"streams":[{"stream":{"job":"test"},"values":[["1","line"]]}]}')
assert_status "Push with revoked key" "401" "$STATUS"

# ── 12. New key still works ────────────────────────────────────────────────────
if [ -n "$NEW_KEY" ]; then
  echo ""
  echo "── 12. Push with new key → 204 ──"
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/akavelog/api/v1/push" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $NEW_KEY" \
    -d "{\"streams\":[{\"stream\":{\"job\":\"e2e-test\"},\"values\":[[\"$TS_NS\",\"line via new key\"]]}]}")
  assert_status "Push with new key" "204" "$STATUS"
fi

# ── 13. Alerts endpoint needs auth ────────────────────────────────────────────
echo ""
echo "── 13. GET /alerts without key → 401 ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/alerts")
assert_status "GET /alerts without key" "401" "$STATUS"

# ── 14. Clean up: delete project ──────────────────────────────────────────────
echo ""
echo "── 14. Delete test project ──"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE/projects/$PROJECT_ID")
assert_status "Delete project" "204" "$STATUS"

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════"
echo " Results: $PASS passed, $FAIL failed"
echo "═══════════════════════════════════════════════"
[ "$FAIL" -eq 0 ]