#!/usr/bin/env bash
# Verify that log batches were uploaded to Akave O3:
# - List objects in the bucket (optionally under logs/default/)
# - Download and decompress the latest batch to show content.
#
# Requires: AWS CLI (https://aws.amazon.com/cli/). O3 is S3-compatible.
#   pip install awscli   OR   https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html
#
# Set credentials (or source from .env):
#   export AWS_ACCESS_KEY_ID="your-O3-access-key"
#   export AWS_SECRET_ACCESS_KEY="your-O3-secret-key"
set -e

# Load O3 vars from backend .env (keys contain dots so we grep)
ENV_FILE="${ENV_FILE:-$(dirname "$0")/../.env}"
if [ -f "$ENV_FILE" ]; then
  export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-$(grep -E '^AKAVELOG_STORAGE\.O3\.ACCESS_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '"')}"
  export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-$(grep -E '^AKAVELOG_STORAGE\.O3\.SECRET_KEY=' "$ENV_FILE" | cut -d= -f2- | tr -d '"')}"
  ENDPOINT="${ENDPOINT:-$(grep -E '^AKAVELOG_STORAGE\.O3\.ENDPOINT=' "$ENV_FILE" | cut -d= -f2- | tr -d '"')}"
  BUCKET="${BUCKET:-$(grep -E '^AKAVELOG_STORAGE\.O3\.BUCKET=' "$ENV_FILE" | cut -d= -f2- | tr -d '"')}"
fi

ENDPOINT="${ENDPOINT:-https://o3-rc2.akave.xyz}"
BUCKET="${BUCKET:-akavelog}"
PREFIX="${1:-logs/default}"

if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
  echo "Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY (or AKAVELOG_STORAGE.O3.* in .env)"
  exit 1
fi

echo "=== List objects in s3://${BUCKET}/${PREFIX} (Akave O3) ==="
aws s3 ls "s3://${BUCKET}/${PREFIX}/" --recursive --endpoint-url "$ENDPOINT" || true

echo ""
echo "=== Download latest batch and show content (optional) ==="
# Find latest .json.gz (by key, newest last in ls output)
LATEST=$(aws s3 ls "s3://${BUCKET}/${PREFIX}/" --recursive --endpoint-url "$ENDPOINT" 2>/dev/null | grep '\.json\.gz$' | tail -1)
if [ -n "$LATEST" ]; then
  # Format: "2024-02-17 12:00:00  123  logs/default/2026/02/17/xxx.json.gz"
  KEY=$(echo "$LATEST" | awk '{print $4}')
  echo "Latest key: $KEY"
  TMP=$(mktemp -t akavelog-batch.XXXXXX.json.gz)
  aws s3 cp "s3://${BUCKET}/${KEY}" "$TMP" --endpoint-url "$ENDPOINT"
  echo "Content (gunzipped JSON):"
  gunzip -c "$TMP" | head -c 2000
  echo ""
  rm -f "$TMP"
else
  echo "No .json.gz objects found under ${PREFIX}/. Run test_ingest_to_cloud.sh first."
fi
