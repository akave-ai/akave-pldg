#!/usr/bin/env bash
# Test: create an HTTP input and push validated logs to the ingest endpoint;
# with O3 configured, batches are uploaded to Akave cloud after flush (size or interval).
set -e
BASE="${BASE_URL:-http://localhost:8080}"

echo "=== 1. Create HTTP input (path /ingest/raw) ==="
CREATE=$(curl -s -X POST "$BASE/inputs" \
  -H "Content-Type: application/json" \
  -d '{"type":"http","title":"test-http","description":"raw"}')
echo "$CREATE" | head -c 500
echo ""
ID=$(echo "$CREATE" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
if [ -z "$ID" ]; then
  echo "Failed to create input (maybe server not running or DB error)"
  exit 1
fi
echo "Created input id: $ID"

echo ""
echo "=== 2. Send 5 valid log payloads to /ingest/raw ==="
for i in 1 2 3 4 5; do
  curl -s -X POST "$BASE/ingest/raw" \
    -H "Content-Type: application/json" \
    -d "{\"service\":\"test-svc\",\"message\":\"log line $i\",\"level\":\"info\",\"tags\":{\"i\":\"$i\"}}"
  echo " -> $i"
done

echo ""
echo "=== 3. Wait 6s for batcher to flush to O3 ==="
sleep 6

echo ""
echo "=== 4. List inputs ==="
curl -s "$BASE/inputs" | head -c 400
echo ""

echo ""
echo "Done. Check server logs for: [batcher] uploaded 5 logs to logs/default/..."
