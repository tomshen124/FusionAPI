#!/usr/bin/env bash
set -euo pipefail

BASE_URL=${BASE_URL:-"http://127.0.0.1:18080"}
API_KEY=${API_KEY:-""}
MODEL=${MODEL:-"gpt-4o-mini"}
STREAM=${STREAM:-"false"}
REQUEST_ID=${REQUEST_ID:-"req_$(date +%s)_$RANDOM"}

if [ -z "$API_KEY" ]; then
  echo "API_KEY is required" >&2
  exit 1
fi

payload=$(cat <<JSON
{
  "model": "${MODEL}",
  "stream": ${STREAM},
  "messages": [
    {"role": "user", "content": "hello (request-id=${REQUEST_ID})"}
  ]
}
JSON
)

echo "POST ${BASE_URL}/v1/chat/completions"

echo "$payload" | curl -sS -D /tmp/fusionapi_headers.txt \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "X-Request-ID: ${REQUEST_ID}" \
  -o /tmp/fusionapi_body.txt \
  --data-binary @- \
  "${BASE_URL}/v1/chat/completions"

echo
resp_rid=$(grep -i '^X-Request-ID:' /tmp/fusionapi_headers.txt | tail -n 1 | awk '{print $2}' | tr -d '\r')
echo "X-Request-ID: ${resp_rid:-<missing>}"

cat /tmp/fusionapi_body.txt
