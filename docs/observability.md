# Observability (P1)

Goal for self/small team mode:

- Any failed/slow request can be classified within 5 minutes using `X-Request-ID`: client vs FusionAPI vs upstream.

## Workflow

1. Client obtains `X-Request-ID` from response headers.
2. Use `/api/logs?request_id=<id>` to drill down.
3. Correlate with stdout structured logs by `request_id`.

## Tools

- Repro request: `scripts/repro_chat.sh`
- Export logs by request-id: `scripts/export_logs.sh`
