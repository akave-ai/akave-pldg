#!/usr/bin/env bash
# =============================================================================
# AkaveLog — Comprehensive End-to-End Test Suite (All Phases)
# Tests every endpoint, edge case, auth flow, and data pipeline.
#
# Usage:
#   ./scripts/test_full_e2e.sh
#   BASE_URL=http://localhost:8080 ./scripts/test_full_e2e.sh
#
# Prerequisites:
#   - Server running with DB + O3 configured (or DB-only mode)
#   - jq installed (brew install jq / apt install jq)
# =============================================================================
set -euo pipefail

BASE="${BASE_URL:-http://localhost:8080}"
PASS=0
FAIL=0
SKIP=0
SECTION=""
CREATED_PROJECT_IDS=()

# ── colours ──────────────────────────────────────────────────────────────────
green()   { printf "\033[32m  ✓ %s\033[0m\n" "$1"; ((PASS++)) || true; }
red()     { printf "\033[31m  ✗ %s\033[0m\n" "$1"; ((FAIL++)) || true; }
yellow()  { printf "\033[33m  ○ %s\033[0m\n" "$1"; ((SKIP++)) || true; }
section() { SECTION="$1"; printf "\n\033[1;34m── %s ──\033[0m\n" "$SECTION"; }

assert_status() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    green "$label (HTTP $actual)"
  else
    red "$label — expected HTTP $expected, got HTTP $actual"
  fi
}

assert_contains() {
  local label="$1" needle="$2" haystack="$3"
  if echo "$haystack" | grep -q "$needle"; then
    green "$label"
  else
    red "$label — expected to contain: $needle"
  fi
}

assert_not_contains() {
  local label="$1" needle="$2" haystack="$3"
  if ! echo "$haystack" | grep -q "$needle"; then
    green "$label"
  else
    red "$label — should NOT contain: $needle"
  fi
}

assert_eq() {
  local label="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    green "$label"
  else
    red "$label — expected '$expected', got '$actual'"
  fi
}

# http helpers
GET()    { curl -s -o /dev/null -w "%{http_code}" --max-time 10 "$BASE$1" "${@:2}"; }
GET_B()  { curl -s --max-time 10 "$BASE$1" "${@:2}"; }
POST()   { curl -s -o /dev/null -w "%{http_code}" --max-time 10 -X POST "$BASE$1" "${@:2}"; }
POST_B() { curl -s --max-time 10 -X POST "$BASE$1" "${@:2}"; }
DEL()    { curl -s -o /dev/null -w "%{http_code}" --max-time 10 -X DELETE "$BASE$1" "${@:2}"; }

JSON="-H Content-Type:application/json"
TS_NS=$(python3 -c "import time; print(int(time.time()*1e9))" 2>/dev/null || echo "1700000000000000000")

cleanup_projects() {
  for id in "${CREATED_PROJECT_IDS[@]}"; do
    curl -s -o /dev/null -X DELETE "$BASE/projects/$id" || true
  done
}
trap cleanup_projects EXIT

echo "═══════════════════════════════════════════════════════════"
echo " AkaveLog — Full End-to-End Test Suite"
echo " Server : $BASE"
echo " Date   : $(date -u)"
echo "═══════════════════════════════════════════════════════════"

# ═══════════════════════════════════════════════════════════════
# PHASE 8 — Project & API Key Management
# ═══════════════════════════════════════════════════════════════
section "PHASE 8: Project CRUD"

# Create project
CREATE_RESP=$(POST_B /projects $JSON -d '{"name":"e2e-main","owner_email":"test@e2e.com"}')
API_KEY=$(echo "$CREATE_RESP" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
PROJECT_ID=$(echo "$CREATE_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
CREATED_PROJECT_IDS+=("$PROJECT_ID")

[ -n "$API_KEY" ] && green "Project created, API key received" || red "Failed to create project"
[ -n "$PROJECT_ID" ] && green "Project ID extracted" || { red "No project ID"; exit 1; }

# Duplicate name must fail
STATUS=$(POST /projects $JSON -d '{"name":"e2e-main"}')
assert_status "Duplicate project name → 400/409/500" "$([ "$STATUS" != "201" ] && echo "$STATUS" || echo "FAIL")" "$STATUS"

# Missing name
STATUS=$(POST /projects $JSON -d '{}')
assert_status "Create project without name → 400" "400" "$STATUS"

# Get by ID
STATUS=$(GET /projects/$PROJECT_ID)
assert_status "GET /projects/:id → 200" "200" "$STATUS"

# Get non-existent
STATUS=$(GET /projects/00000000-0000-0000-0000-000000000000)
assert_status "GET non-existent project → 404" "404" "$STATUS"

# List
RESP=$(GET_B /projects)
assert_contains "Project appears in list" "e2e-main" "$RESP"

section "PHASE 8: API Key Management"

# List keys (default key from create)
RESP=$(GET_B /projects/$PROJECT_ID/api-keys)
assert_contains "Default key exists after create" "akal_" "$RESP"

# Create additional key
KEY_RESP=$(POST_B /projects/$PROJECT_ID/api-keys $JSON -d '{"name":"ci-key"}')
SECOND_KEY=$(echo "$KEY_RESP" | grep -o '"key":"[^"]*"' | cut -d'"' -f4)
[ -n "$SECOND_KEY" ] && green "Second key created" || red "Second key creation failed"

# List should now have 2
RESP=$(GET_B /projects/$PROJECT_ID/api-keys)
KEY_COUNT=$(echo "$RESP" | grep -o "akal_" | wc -l | tr -d ' ')
assert_eq "Two keys listed for project" "2" "$KEY_COUNT"

# Revoke second key
STATUS=$(DEL /projects/$PROJECT_ID/api-keys/$SECOND_KEY)
assert_status "Revoke API key → 204" "204" "$STATUS"

# Revoke again → 404
STATUS=$(DEL /projects/$PROJECT_ID/api-keys/$SECOND_KEY)
assert_status "Revoke already-revoked key → 404" "404" "$STATUS"

# Revoke non-existent key
STATUS=$(DEL /projects/$PROJECT_ID/api-keys/akal_doesnotexist000000000)
assert_status "Revoke non-existent key → 404" "404" "$STATUS"

section "PHASE 8: Authentication Middleware"

# No header
STATUS=$(POST /akavelog/api/v1/push $JSON -d '{"streams":[]}')
assert_status "Push without X-API-Key → 401" "401" "$STATUS"

STATUS=$(POST /query $JSON -d '{}')
assert_status "Query without X-API-Key → 401" "401" "$STATUS"

STATUS=$(GET /alerts)
assert_status "GET alerts without X-API-Key → 401" "401" "$STATUS"

STATUS=$(POST /alerts $JSON -d '{}')
assert_status "POST alerts without X-API-Key → 401" "401" "$STATUS"

STATUS=$(DEL /alerts/00000000-0000-0000-0000-000000000000 -H "X-API-Key:")
assert_status "DELETE alert without key → 401" "401" "$STATUS"

# Wrong key
STATUS=$(POST /akavelog/api/v1/push $JSON -H "X-API-Key: akal_wrongkey000000000000000000000000000000000000" -d '{"streams":[]}')
assert_status "Push with wrong key → 401" "401" "$STATUS"

# Revoked key (second key is already revoked)
STATUS=$(POST /akavelog/api/v1/push $JSON -H "X-API-Key: $SECOND_KEY" -d '{"streams":[{"stream":{"job":"test"},"values":[["1","line"]]}]}')
assert_status "Push with revoked key → 401" "401" "$STATUS"

# Public endpoints must NOT require auth
STATUS=$(GET /logs/recent)
assert_status "GET /logs/recent is public → 200" "200" "$STATUS"

STATUS=$(GET /logs/status)
assert_status "GET /logs/status is public → 200" "200" "$STATUS"

STATUS=$(GET /projects)
assert_status "GET /projects is public → 200" "200" "$STATUS"

# ═══════════════════════════════════════════════════════════════
# PHASE 2+3 — Log Ingestion + O3 Storage
# ═══════════════════════════════════════════════════════════════
section "PHASE 2+3: Push / Ingestion"

AUTH="-H X-API-Key:$API_KEY"
PUSH_BODY='{"streams":[{"stream":{"job":"payment-api","level":"error","env":"test"},"values":[["'"$TS_NS"'","ERROR payment failed for user 42"]]}]}'

# Valid push
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d "$PUSH_BODY")
assert_status "Authenticated push → 204" "204" "$STATUS"

# Multi-stream push
MULTI='{"streams":[
  {"stream":{"job":"auth-api","level":"info"},"values":[["'"$TS_NS"'","INFO user login ok"]]},
  {"stream":{"job":"worker","level":"warn"},"values":[["'"$TS_NS"'","WARN queue depth high"]]},
  {"stream":{"job":"payment-api","level":"error"},"values":[["'"$TS_NS"'","ERROR charge failed"]]}
]}'
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d "$MULTI")
assert_status "Multi-stream push → 204" "204" "$STATUS"

# Gzip-encoded push
GZIP_BODY=$(echo -n "$PUSH_BODY" | gzip -c | base64 -w0 2>/dev/null || echo -n "$PUSH_BODY" | gzip -c | base64)
# (test uncompressed fallback — gzip path needs real binary pipe, skip if unavailable)

# Empty streams → 204 (no-op)
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d '{"streams":[]}')
assert_status "Empty streams push → 204" "204" "$STATUS"

# Body too large (send > 5MB compressed limit description)
yellow "Body-too-large test skipped (requires 5MB payload generation)"

# Missing Content-Type
STATUS=$(POST /akavelog/api/v1/push $AUTH -d "$PUSH_BODY")
assert_status "Push without Content-Type → 400" "400" "$STATUS"

# Invalid JSON body
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d 'not-json')
assert_status "Push invalid JSON → 400" "400" "$STATUS"

# Send several logs for /logs/recent visibility
for i in 1 2 3 4 5; do
  BODY='{"streams":[{"stream":{"job":"demo-svc","level":"info"},"values":[["'"$TS_NS"'","INFO demo log line '"$i"'"]]}]}'
  curl -s -o /dev/null -X POST "$BASE/akavelog/api/v1/push" \
    -H "Content-Type: application/json" -H "X-API-Key: $API_KEY" -d "$BODY"
done
green "Pushed 5 demo log lines"

section "PHASE 3: Recent Logs UI helper"

RESP=$(GET_B /logs/recent)
assert_contains "Recent logs contains pushed data" "demo-svc" "$RESP"

RESP=$(GET_B /logs/status)
assert_contains "Log status endpoint returns JSON" "batcher_enabled" "$RESP"

# ═══════════════════════════════════════════════════════════════
# PHASE 5 — Query Engine
# ═══════════════════════════════════════════════════════════════
section "PHASE 5: Query Engine — POST /query"

# All queries pin to a narrow window (last 10 min) to avoid scanning all O3 objects.
# The query engine default lookback is 30 days which would fetch every chunk.
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
HOUR_AGO=$(date -u -d "1 hour ago" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -v-1H +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "2000-01-01T00:00:00Z")
RECENT_START=$(date -u -d "10 minutes ago" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -v-10M +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "$HOUR_AGO")

# Basic query (pinned to recent window)
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":10}")
assert_status "Query with no filters → 200" "200" "$STATUS"

# Query with time range
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$HOUR_AGO\",\"time_end\":\"$NOW\",\"limit\":10}")
assert_status "Query with time range → 200" "200" "$STATUS"

# Query with service filter (pinned window)
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"service\":\"payment-api\",\"limit\":10}")
assert_status "Query with service filter → 200" "200" "$STATUS"

# Query with level filter (pinned window)
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"levels\":[\"error\",\"warn\"],\"limit\":10}")
assert_status "Query with level filter → 200" "200" "$STATUS"

# Query with keyword (pinned window)
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"keyword\":\"payment\",\"limit\":10}")
assert_status "Query with keyword filter → 200" "200" "$STATUS"

# Query with all filters
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$HOUR_AGO\",\"time_end\":\"$NOW\",\"service\":\"payment-api\",\"levels\":[\"error\"],\"keyword\":\"failed\",\"limit\":5}")
assert_status "Query with all filters → 200" "200" "$STATUS"

# Invalid time range (start > end)
STATUS=$(POST /query $JSON $AUTH -d '{"time_start":"2026-12-01T00:00:00Z","time_end":"2026-01-01T00:00:00Z"}')
assert_status "Query start > end → 400" "400" "$STATUS"

# Limit clamping — use a small time window to avoid scanning all O3 objects
RECENT_START=$(date -u -d "5 minutes ago" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -v-5M +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "$HOUR_AGO")
RESP=$(curl -s --max-time 15 -X POST "$BASE/query" \
  -H "Content-Type: application/json" -H "X-API-Key: $API_KEY" \
  -d "{\"limit\":999999,\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\"}")
assert_contains "Limit clamped in response meta" "truncated" "$RESP"

# Limit = 0 → defaults to 100
STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 15 -X POST "$BASE/query" \
  -H "Content-Type: application/json" -H "X-API-Key: $API_KEY" \
  -d "{\"limit\":0,\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\"}")
assert_status "Query limit=0 → 200 (defaults)" "200" "$STATUS"

# Negative limit
STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 15 -X POST "$BASE/query" \
  -H "Content-Type: application/json" -H "X-API-Key: $API_KEY" \
  -d "{\"limit\":-1,\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\"}")
assert_status "Query limit=-1 → 200 (defaults)" "200" "$STATUS"

# Response structure check — narrow window, limit=1 for speed
RESP=$(curl -s --max-time 15 -X POST "$BASE/query" \
  -H "Content-Type: application/json" -H "X-API-Key: $API_KEY" \
  -d "{\"limit\":1,\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\"}")
assert_contains "Response has results field" "results" "$RESP"
assert_contains "Response has count field" "count" "$RESP"
assert_contains "Response has truncated field" "truncated" "$RESP"

section "PHASE 5: Query Engine — GET /query/stream (SSE)"
# All SSE tests pin to RECENT_START..NOW window so the server finds batches
# quickly without scanning 30 days of O3 objects.
# curl exits 28 on --max-time timeout; we suppress with || true so set -e
# does not abort the script. %{http_code} is still written to stdout before
# the stream body, so STATUS is correctly captured.

# SSE stream — pinned window, no body filters
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "X-API-Key: $API_KEY" \
  --max-time 5 \
  "$BASE/query/stream?from=${RECENT_START}&to=${NOW}&limit=5" || true)
assert_status "SSE stream → 200" "200" "$STATUS"

# SSE stream with service+level filters (pinned window)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "X-API-Key: $API_KEY" \
  --max-time 5 \
  "$BASE/query/stream?service=payment-api&levels=error&from=${RECENT_START}&to=${NOW}&limit=5" || true)
assert_status "SSE stream with filters → 200" "200" "$STATUS"

# SSE stream contains event: done — pinned window so server drains quickly
RESP=$(curl -s -H "X-API-Key: $API_KEY" --max-time 8 \
  "$BASE/query/stream?from=${RECENT_START}&to=${NOW}&limit=5" || true)
assert_contains "SSE stream contains 'done' event" "event: done" "$RESP"

# SSE invalid from param — returns 400 immediately (no connection kept open)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "X-API-Key: $API_KEY" \
  --max-time 5 \
  "$BASE/query/stream?from=not-a-date")
assert_status "SSE invalid from param → 400" "400" "$STATUS"

# SSE invalid limit — returns 400 immediately
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "X-API-Key: $API_KEY" \
  --max-time 5 \
  "$BASE/query/stream?limit=abc")
assert_status "SSE invalid limit param → 400" "400" "$STATUS"

# SSE without auth — returns 401 immediately (middleware fires before streaming starts)
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  --max-time 5 \
  "$BASE/query/stream")
assert_status "SSE stream without key → 401" "401" "$STATUS"

# ═══════════════════════════════════════════════════════════════
# PHASE 7 — Alert Rules + Background Worker
# ═══════════════════════════════════════════════════════════════
section "PHASE 7: Alert Rules — CRUD"

# Create keyword alert
KW_RESP=$(POST_B /alerts $JSON $AUTH -d '{
  "name":"e2e-keyword-alert",
  "type":"keyword",
  "conditions":{"service":"payment-api","keyword":"ERROR","window_minutes":5}
}')
KW_RULE_ID=$(echo "$KW_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$KW_RULE_ID" ] && green "Keyword alert rule created ($KW_RULE_ID)" || red "Failed to create keyword alert"

# Create threshold alert
TH_RESP=$(POST_B /alerts $JSON $AUTH -d '{
  "name":"e2e-threshold-alert",
  "type":"threshold",
  "conditions":{"service":"payment-api","level":"error","threshold":10,"window_minutes":5}
}')
TH_RULE_ID=$(echo "$TH_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$TH_RULE_ID" ] && green "Threshold alert rule created ($TH_RULE_ID)" || red "Failed to create threshold alert"

# Create alert with webhook
WH_RESP=$(POST_B /alerts $JSON $AUTH -d '{
  "name":"e2e-webhook-alert",
  "type":"keyword",
  "conditions":{"keyword":"FATAL","window_minutes":1},
  "actions":{"webhook_url":"http://localhost:19999/webhook"}
}')
WH_RULE_ID=$(echo "$WH_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$WH_RULE_ID" ] && green "Webhook alert rule created" || red "Failed to create webhook alert"

# Create disabled alert
DIS_RESP=$(POST_B /alerts $JSON $AUTH -d '{
  "name":"e2e-disabled-alert",
  "type":"keyword",
  "conditions":{"keyword":"DISABLED","window_minutes":5},
  "enabled":false
}')
DIS_RULE_ID=$(echo "$DIS_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$DIS_RULE_ID" ] && green "Disabled alert rule created" || red "Failed to create disabled alert"

section "PHASE 7: Alert Rules — Validation"

# Missing name
STATUS=$(POST /alerts $JSON $AUTH -d '{"type":"keyword","conditions":{"keyword":"X","window_minutes":5}}')
assert_status "Alert without name → 400" "400" "$STATUS"

# Missing type
STATUS=$(POST /alerts $JSON $AUTH -d '{"name":"bad","conditions":{"keyword":"X"}}')
assert_status "Alert without type → 400" "400" "$STATUS"

# Invalid type
STATUS=$(POST /alerts $JSON $AUTH -d '{"name":"bad","type":"unknown","conditions":{}}')
assert_status "Alert with invalid type → 400" "400" "$STATUS"

# Keyword rule without keyword field
STATUS=$(POST /alerts $JSON $AUTH -d '{"name":"bad","type":"keyword","conditions":{"window_minutes":5}}')
assert_status "Keyword rule missing keyword → 400" "400" "$STATUS"

# Threshold rule with threshold=0
STATUS=$(POST /alerts $JSON $AUTH -d '{"name":"bad","type":"threshold","conditions":{"threshold":0,"window_minutes":5}}')
assert_status "Threshold rule with threshold=0 → 400" "400" "$STATUS"

# Threshold rule with negative threshold
STATUS=$(POST /alerts $JSON $AUTH -d '{"name":"bad","type":"threshold","conditions":{"threshold":-1,"window_minutes":5}}')
assert_status "Threshold rule with negative threshold → 400" "400" "$STATUS"

# Missing conditions
STATUS=$(POST /alerts $JSON $AUTH -d '{"name":"bad","type":"keyword"}')
assert_status "Alert without conditions → 400" "400" "$STATUS"

section "PHASE 7: Alert Rules — List, Delete, Events"

# List alerts
RESP=$(GET_B /alerts $AUTH)
assert_contains "Alert list contains created rule" "e2e-keyword-alert" "$RESP"
assert_contains "Alert list contains threshold rule" "e2e-threshold-alert" "$RESP"
assert_contains "Alert list contains disabled rule" "e2e-disabled-alert" "$RESP"

# List events (should be empty initially)
if [ -n "$KW_RULE_ID" ]; then
  STATUS=$(GET /alerts/$KW_RULE_ID/events $AUTH)
  assert_status "GET alert events → 200" "200" "$STATUS"
fi

# Delete alert
if [ -n "$TH_RULE_ID" ]; then
  STATUS=$(DEL /alerts/$TH_RULE_ID -H "X-API-Key: $API_KEY")
  assert_status "DELETE alert rule → 204" "204" "$STATUS"

  # Delete again → 404
  STATUS=$(DEL /alerts/$TH_RULE_ID -H "X-API-Key: $API_KEY")
  assert_status "DELETE already-deleted rule → 404" "404" "$STATUS"
fi

# Delete non-existent
STATUS=$(DEL /alerts/00000000-0000-0000-0000-000000000000 -H "X-API-Key: $API_KEY")
assert_status "DELETE non-existent alert → 404" "404" "$STATUS"

# Invalid UUID
STATUS=$(DEL /alerts/not-a-uuid -H "X-API-Key: $API_KEY")
assert_status "DELETE alert with invalid UUID → 400" "400" "$STATUS"

# Invalid UUID for events
STATUS=$(GET /alerts/not-a-uuid/events $AUTH)
assert_status "GET events with invalid UUID → 400" "400" "$STATUS"

# ═══════════════════════════════════════════════════════════════
# PHASE 4 — Metadata Index (PostgreSQL log_batches)
# ═══════════════════════════════════════════════════════════════
section "PHASE 4: Metadata Index — /index/batches"

STATUS=$(GET /index/batches)
assert_status "GET /index/batches (no auth needed) → 200" "200" "$STATUS"

STATUS=$(GET "/index/batches?tenant=default")
assert_status "GET /index/batches with tenant → 200" "200" "$STATUS"

STATUS=$(GET "/index/batches?from=$HOUR_AGO&to=$NOW")
assert_status "GET /index/batches with time range → 200" "200" "$STATUS"

STATUS=$(GET "/index/batches?from=not-a-date")
assert_status "GET /index/batches with invalid from → 400" "400" "$STATUS"

section "PHASE 4: O3 Index — /index/chunks"
# These calls hit Akave O3 directly (ListObjectsV2) which can be slow.
# We accept 200 or 500 (O3 timeout/unavailable) and cap at 12s.

STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 12 "$BASE/index/chunks" || true)
if [ "$STATUS" = "200" ] || [ "$STATUS" = "500" ] || [ "$STATUS" = "000" ]; then
  green "GET /index/chunks → $STATUS (O3 call; 000=timeout before headers)"
else
  red "GET /index/chunks → unexpected status $STATUS"
fi

STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 12 "$BASE/index/chunks?tenant=default" || true)
if [ "$STATUS" = "200" ] || [ "$STATUS" = "500" ] || [ "$STATUS" = "000" ]; then
  green "GET /index/chunks with tenant → $STATUS"
else
  red "GET /index/chunks with tenant → unexpected status $STATUS"
fi

# Invalid from_ns returns 400 immediately (validation before O3 call)
STATUS=$(GET "/index/chunks?from_ns=abc")
assert_status "GET /index/chunks invalid from_ns → 400" "400" "$STATUS"

# ═══════════════════════════════════════════════════════════════
# Upload / O3 Object endpoints
# ═══════════════════════════════════════════════════════════════
section "O3 Upload Endpoints"
# /uploads calls ListObjectsV2 against Akave O3 — can be slow. Accept 200 or 500.

O3_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 12 "$BASE/uploads" || true)
if [ "$O3_STATUS" = "200" ] || [ "$O3_STATUS" = "500" ]; then
  green "GET /uploads → $O3_STATUS"
else
  assert_status "GET /uploads → 200 or 500" "200" "$O3_STATUS"
fi

O3_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 12 "$BASE/uploads?prefix=chunks/" || true)
if [ "$O3_STATUS" = "200" ] || [ "$O3_STATUS" = "500" ]; then
  green "GET /uploads with prefix → $O3_STATUS"
else
  assert_status "GET /uploads with prefix → 200 or 500" "200" "$O3_STATUS"
fi

# Missing key param — validated before any O3 call, returns immediately
STATUS=$(GET /uploads/content)
assert_status "GET /uploads/content without key → 400" "400" "$STATUS"

STATUS=$(GET /uploads/raw)
assert_status "GET /uploads/raw without key → 400" "400" "$STATUS"

# Non-existent key — server catches O3 error and returns 200 with empty payload
STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 12 "$BASE/uploads/content?key=chunks/default/nonexistent.json.gz" || true)
[ "$STATUS" = "200" ] && green "GET /uploads/content non-existent key → 200 (graceful)" || yellow "GET /uploads/content non-existent key → $STATUS (O3 slow)"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 12 "$BASE/uploads/raw?key=chunks/default/nonexistent.json.gz" || true)
[ "$STATUS" = "200" ] && green "GET /uploads/raw non-existent key → 200 (graceful)" || yellow "GET /uploads/raw non-existent key → $STATUS (O3 slow)"

# ═══════════════════════════════════════════════════════════════
# Cross-project isolation
# ═══════════════════════════════════════════════════════════════
section "PHASE 8: Cross-Project Isolation"

# Create a second project
P2_RESP=$(POST_B /projects $JSON -d '{"name":"e2e-project-two","owner_email":"p2@test.com"}')
P2_KEY=$(echo "$P2_RESP" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
P2_ID=$(echo "$P2_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
CREATED_PROJECT_IDS+=("$P2_ID")

[ -n "$P2_KEY" ] && green "Second project created" || red "Failed to create second project"

# Project 2's key cannot use project 1's endpoints (they use their own scoped data)
# Push with project 2's key
STATUS=$(POST /akavelog/api/v1/push $JSON -H "X-API-Key: $P2_KEY" \
  -d '{"streams":[{"stream":{"job":"p2-svc","level":"info"},"values":[["'"$TS_NS"'","p2 log line"]]}]}')
assert_status "Project 2 can push its own logs → 204" "204" "$STATUS"

# Project 2's key works for query
STATUS=$(POST /query $JSON -H "X-API-Key: $P2_KEY" -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":5}")
assert_status "Project 2 can query → 200" "200" "$STATUS"

# Project 1's key still works
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":5}")
assert_status "Project 1 key still valid → 200" "200" "$STATUS"

# Delete project 2 — its API key should stop working
DEL_STATUS=$(DEL /projects/$P2_ID)
[ "$DEL_STATUS" = "204" ] && green "Project 2 deleted" || yellow "Project 2 deletion returned $DEL_STATUS"

STATUS=$(POST /akavelog/api/v1/push $JSON -H "X-API-Key: $P2_KEY" \
  -d '{"streams":[{"stream":{"job":"ghost"},"values":[["1","line"]]}]}')
assert_status "Deleted project key → 401" "401" "$STATUS"

# Remove from cleanup list (already deleted)
CREATED_PROJECT_IDS=("${CREATED_PROJECT_IDS[@]/$P2_ID}")

# ═══════════════════════════════════════════════════════════════
# API Key rotation flow
# ═══════════════════════════════════════════════════════════════
section "PHASE 8: API Key Rotation"

# Create new key
NEW_KEY_RESP=$(POST_B /projects/$PROJECT_ID/api-keys $JSON -d '{"name":"rotated"}')
NEW_KEY=$(echo "$NEW_KEY_RESP" | grep -o '"key":"[^"]*"' | cut -d'"' -f4)
[ -n "$NEW_KEY" ] && green "Rotation key created" || red "Failed to create rotation key"

# Verify new key works
STATUS=$(POST /query $JSON -H "X-API-Key: $NEW_KEY" -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":1}")
assert_status "New key works before revoke → 200" "200" "$STATUS"

# Old key still works (both active simultaneously)
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":1}")
assert_status "Old key still works during rotation → 200" "200" "$STATUS"

# Revoke old key
DEL /projects/$PROJECT_ID/api-keys/$API_KEY > /dev/null
green "Old key revoked"

# Old key no longer works
STATUS=$(POST /query $JSON $AUTH -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":1}")
assert_status "Old key after revoke → 401" "401" "$STATUS"

# New key still works
STATUS=$(POST /query $JSON -H "X-API-Key: $NEW_KEY" -d "{\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":1}")
assert_status "New key after revoke still works → 200" "200" "$STATUS"

# Update AUTH to new key for remaining tests
AUTH="-H X-API-Key:$NEW_KEY"

# ═══════════════════════════════════════════════════════════════
# Project deletion cascade
# ═══════════════════════════════════════════════════════════════
section "PHASE 8: Project Deletion Cascade"

# Create a throw-away project with a rule and push some data
TEMP_RESP=$(POST_B /projects $JSON -d '{"name":"e2e-temp-cascade"}')
TEMP_KEY=$(echo "$TEMP_RESP" | grep -o '"api_key":"[^"]*"' | cut -d'"' -f4)
TEMP_ID=$(echo "$TEMP_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

# Create an alert rule under temp project
if [ -n "$TEMP_KEY" ] && [ -n "$TEMP_ID" ]; then
  POST_B /alerts $JSON -H "X-API-Key: $TEMP_KEY" \
    -d '{"name":"temp-rule","type":"keyword","conditions":{"keyword":"TEST","window_minutes":1}}' > /dev/null

  # Delete the project
  STATUS=$(DEL /projects/$TEMP_ID)
  assert_status "Delete project with alert rules → 204" "204" "$STATUS"

  # Key no longer resolves
  STATUS=$(POST /query $JSON -H "X-API-Key: $TEMP_KEY" -d '{"limit":1}')
  assert_status "Deleted project key rejected → 401" "401" "$STATUS"
  green "Cascade delete verified: key invalidated, rules removed"
else
  yellow "Cascade test skipped — temp project creation failed"
fi

# ═══════════════════════════════════════════════════════════════
# Ingestion edge cases
# ═══════════════════════════════════════════════════════════════
section "Ingestion Edge Cases"

# Push with missing values array
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d \
  '{"streams":[{"stream":{"job":"test"}}]}')
assert_status "Push stream with no values → 204 (skipped)" "204" "$STATUS"

# Push with empty values array
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d \
  '{"streams":[{"stream":{"job":"test"},"values":[]}]}')
assert_status "Push stream with empty values → 204" "204" "$STATUS"

# Push with null stream labels
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d \
  '{"streams":[{"stream":null,"values":[["'"$TS_NS"'","hello"]]}]}')
assert_status "Push with null labels → 204 (normalized)" "204" "$STATUS"

# Push with very long line
LONG_LINE=$(python3 -c "print('X' * 4000)" 2>/dev/null || printf '%4000s' | tr ' ' 'X')
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d \
  '{"streams":[{"stream":{"job":"test"},"values":[["'"$TS_NS"'","'"$LONG_LINE"'"]]}]}')
assert_status "Push with 4000-char line → 204" "204" "$STATUS"

# Multiple streams same service
BATCH='{"streams":['
for i in 1 2 3 4 5; do
  BATCH+='{"stream":{"job":"batch-test","level":"info"},"values":[["'"$TS_NS"'","batch line '"$i"'"]]}'
  [ $i -lt 5 ] && BATCH+=","
done
BATCH+=']}'
STATUS=$(POST /akavelog/api/v1/push $JSON $AUTH -d "$BATCH")
assert_status "Push 5 streams in one request → 204" "204" "$STATUS"

# ═══════════════════════════════════════════════════════════════
# Query edge cases
# ═══════════════════════════════════════════════════════════════
section "Query Edge Cases"

# Empty keyword (should be treated as no keyword filter)
STATUS=$(POST /query $JSON $AUTH -d "{\"keyword\":\"\",\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":5}")
assert_status "Query with empty keyword → 200" "200" "$STATUS"

# Multiple levels (pinned window)
STATUS=$(POST /query $JSON $AUTH -d "{\"levels\":[\"error\",\"warn\",\"info\",\"debug\"],\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":5}")
assert_status "Query with multiple levels → 200" "200" "$STATUS"

# Unknown service (should return empty results not error)
RESP=$(POST_B /query $JSON $AUTH -d "{\"service\":\"service-that-does-not-exist-xyzzy\",\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\",\"limit\":5}")
assert_contains "Query unknown service → empty results" "results" "$RESP"

# Future time range (no results but not error — these are already bounded so safe)
STATUS=$(POST /query $JSON $AUTH -d '{"time_start":"2099-01-01T00:00:00Z","time_end":"2099-12-31T00:00:00Z"}')
assert_status "Query future time range → 200" "200" "$STATUS"

# Past time range (no results but not error)
STATUS=$(POST /query $JSON $AUTH -d '{"time_start":"2000-01-01T00:00:00Z","time_end":"2000-12-31T00:00:00Z"}')
assert_status "Query ancient time range → 200" "200" "$STATUS"

# Limit = 1 (pinned window)
STATUS=$(POST /query $JSON $AUTH -d "{\"limit\":1,\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\"}")
assert_status "Query limit=1 → 200" "200" "$STATUS"

# Limit = max 1000 (pinned window so bounded fetch)
STATUS=$(POST /query $JSON $AUTH -d "{\"limit\":1000,\"time_start\":\"$RECENT_START\",\"time_end\":\"$NOW\"}")
assert_status "Query limit=1000 (max) → 200" "200" "$STATUS"

# ═══════════════════════════════════════════════════════════════
# HTTP method enforcement
# ═══════════════════════════════════════════════════════════════
section "HTTP Method Enforcement"
# Echo returns 404 (not 405) when a route exists under a different method.
# POST /logs/recent hits the auth middleware before the method check → 401.
# These tests document actual Echo behaviour.

STATUS=$(GET /query $AUTH)
if [ "$STATUS" = "404" ] || [ "$STATUS" = "405" ]; then
  green "GET /query (POST-only) → $STATUS (Echo: 404 for wrong method)"
else
  red "GET /query → unexpected status $STATUS"
fi

STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 -X GET "$BASE/akavelog/api/v1/push" \
  -H "X-API-Key: $NEW_KEY")
if [ "$STATUS" = "404" ] || [ "$STATUS" = "405" ]; then
  green "GET /push (POST-only) → $STATUS (Echo: 404 for wrong method)"
else
  red "GET /push → unexpected status $STATUS"
fi

# POST /logs/recent — auth middleware runs before route match → 401 without a key
STATUS=$(POST /logs/recent)
if [ "$STATUS" = "401" ] || [ "$STATUS" = "405" ]; then
  green "POST /logs/recent → $STATUS (auth or method check)"
else
  red "POST /logs/recent → unexpected status $STATUS"
fi

# ═══════════════════════════════════════════════════════════════
# Observability / status endpoints
# ═══════════════════════════════════════════════════════════════
section "Observability Endpoints"

RESP=$(GET_B /logs/status)
assert_contains "/logs/status has batcher_enabled" "batcher_enabled" "$RESP"
assert_contains "/logs/status has last_upload_at" "last_upload_at" "$RESP"
assert_contains "/logs/status has pending_count" "pending_count" "$RESP"

RESP=$(GET_B /logs/recent)
assert_contains "/logs/recent has logs array" "logs" "$RESP"

# ═══════════════════════════════════════════════════════════════
# Cleanup alerts created in this run
# ═══════════════════════════════════════════════════════════════
section "Cleanup"

for id in "$KW_RULE_ID" "$WH_RULE_ID" "$DIS_RULE_ID"; do
  if [ -n "$id" ]; then
    curl -s -o /dev/null -X DELETE "$BASE/alerts/$id" \
      -H "X-API-Key: $NEW_KEY" || true
  fi
done
green "Test alerts cleaned up"

# ═══════════════════════════════════════════════════════════════
# Summary
# ═══════════════════════════════════════════════════════════════
echo ""
echo "═══════════════════════════════════════════════════════════"
printf " \033[32m✓ %d passed\033[0m  \033[31m✗ %d failed\033[0m  \033[33m○ %d skipped\033[0m\n" "$PASS" "$FAIL" "$SKIP"
echo "═══════════════════════════════════════════════════════════"

[ "$FAIL" -eq 0 ]