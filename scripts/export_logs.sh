#!/usr/bin/env bash
set -euo pipefail

ADMIN_BASE_URL=${ADMIN_BASE_URL:-"http://127.0.0.1:18080"}
ADMIN_KEY=${ADMIN_KEY:-""}
REQUEST_ID=${REQUEST_ID:-""}
LIMIT=${LIMIT:-100}

if [ -z "$ADMIN_KEY" ]; then
  echo "ADMIN_KEY is required" >&2
  exit 1
fi
if [ -z "$REQUEST_ID" ]; then
  echo "REQUEST_ID is required" >&2
  exit 1
fi

out_dir=${OUT_DIR:-"out"}
mkdir -p "$out_dir"
out_file="$out_dir/request_${REQUEST_ID}.json"

curl -sS \
  -H "Authorization: Bearer ${ADMIN_KEY}" \
  "${ADMIN_BASE_URL}/api/logs?request_id=${REQUEST_ID}&limit=${LIMIT}" \
  > "$out_file"

echo "saved: $out_file"
