# FusionAPI

A lightweight AI API aggregation gateway built with Go and Vue 3.

FusionAPI unifies multiple upstream providers (NewAPI, CPA, OpenAI, Anthropic, Custom OpenAI-compatible APIs) behind one OpenAI-compatible entrypoint, with health checks, failover, routing strategies, and a Web UI.

## Features

- Multi-source aggregation through one API gateway
- OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/models`)
- Routing strategies: `priority`, `round-robin`, `weighted`, `least-latency`, `least-cost`
- Automatic failover when upstream sources fail
- Function Calling and Extended Thinking capability-aware routing
- FC fallback degradation: when no FC-capable source is available, request can fallback to a non-FC source and remove tool fields
- CPA-specific adaptation with provider-aware FC capability checks
- CPA model/provider auto-detection from `/v1/models`
- Runtime config updates from Web UI with persistence to `config.yaml`
- Optional auth for both proxy API and admin API
- Lightweight deployment (single binary + SQLite)

## Quick Start

### Build from source

```bash
git clone https://github.com/YOUR_USER/FusionAPI.git
cd FusionAPI

# backend
go build -o fusionapi ./cmd/fusionapi

# frontend
cd web
npm install
npm run build
cd ..

./fusionapi
```

Open `http://localhost:8080`.

### Docker

```bash
docker-compose up -d
```

Or:

```bash
docker build -t fusionapi .
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  fusionapi
```

## Configuration

Edit `config.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  api_key: ""          # protects /v1/*; empty = disabled; "auto" = generate at startup
  admin_api_key: ""    # protects /api/*; empty = disabled; "auto" = generate at startup

database:
  path: "./data/fusion.db"

health_check:
  enabled: true
  interval: 60
  timeout: 10
  failure_threshold: 3

routing:
  strategy: "priority"
  failover:
    enabled: true
    max_retries: 2

logging:
  level: "info"
  retention_days: 7

sources: []
```

### Auth behavior

- `server.api_key` protects `/v1/*`
- `server.admin_api_key` protects `/api/*`
- Empty value means no auth for that scope
- `"auto"` means key is generated on startup and written back to `config.yaml`

### Web UI admin key experience

If `/api/*` returns `401`, Web UI prompts for `admin_api_key` once and stores it in browser local storage.

## Source Types

| Type | Identifier | Description |
|------|------------|-------------|
| NewAPI | `newapi` | one-api/new-api relay |
| CPA | `cpa` | CLIProxyAPI reverse proxy |
| OpenAI | `openai` | OpenAI official API |
| Anthropic | `anthropic` | Anthropic official API |
| Custom | `custom` | Any OpenAI-compatible API |

## CPA Behavior

CPA sources have special handling:

- Extended Thinking is not supported (thinking field is removed)
- Function Calling support is provider-dependent
- Optional API key (if empty, Authorization header is not sent)
- Auto-detect model-to-provider mapping from `/v1/models`
- `cpa.providers` and `cpa.account_mode` participate in effective routing eligibility

Provider capability matrix:

| Provider | FC | Vision |
|----------|----|--------|
| gemini   | yes | yes |
| claude   | yes | yes |
| codex    | yes | yes |
| qwen     | no  | yes |

## API Endpoints

### Proxy API (OpenAI-compatible)

- `POST /v1/chat/completions`
- `GET /v1/models`

Example:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Admin API

- `GET/POST /api/sources`
- `GET/PUT/DELETE /api/sources/:id`
- `POST /api/sources/:id/test`
- `GET /api/sources/:id/balance`
- `GET /api/status`
- `GET /api/health`
- `GET /api/logs`
- `GET /api/stats`
- `GET/PUT /api/config`

When `admin_api_key` is set:

```bash
curl http://localhost:8080/api/status \
  -H "Authorization: Bearer your-admin-api-key"
```

## Production Notes

For public internet deployment:

- Set non-empty `api_key` and `admin_api_key`
- Prefer HTTPS reverse proxy (Nginx/Caddy)
- Restrict admin API access by source IP or additional proxy auth
- Keep database and config files on persistent volumes

## Development

```bash
# backend tests
GOCACHE=$(pwd)/.cache/go-build go test ./...

# frontend build
cd web && npm run build
```

## Project Structure

```text
FusionAPI/
├── cmd/fusionapi/main.go
├── internal/
│   ├── api/
│   ├── config/
│   ├── core/
│   ├── model/
│   └── store/
├── web/
├── config.yaml
├── Dockerfile
└── docker-compose.yml
```

## License

MIT
