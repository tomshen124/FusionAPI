# CLAUDE.md

This file provides guidance to coding agents when working with code in this repository.

## Project Overview

FusionAPI is a lightweight AI API aggregation gateway written in Go with a Vue3 Web UI. It unifies multiple AI API sources (NewAPI, CPA, OpenAI, Anthropic, Custom) behind a single OpenAI-compatible endpoint with intelligent routing, health checking, failover, and FC degradation fallback.

## Build & Run Commands

```bash
# Backend (Go)
go build -o fusionapi ./cmd/fusionapi
go run ./cmd/fusionapi

# Run tests
go test ./...
go test -v ./internal/core/...    # test specific package
go test -run TestRouter ./...     # run specific test

# Frontend (Vue3)
cd web && npm install
cd web && npm run dev      # development (proxy to :8080)
cd web && npm run build    # production build (output to dist/web/)

# Docker
docker-compose up -d
```

## Architecture

```
┌─────────────────┐     ┌──────────────────────────────────────────┐
│   Web UI        │     │              FusionAPI Core              │
│   (Vue3)        │────>│                                          │
└─────────────────┘     │  API Server (/v1/*, /api/*)              │
                        │       │                                   │
                        │  ┌────┴────┐                              │
                        │  │ Router  │<── Health Checker            │
                        │  └────┬────┘                              │
                        │       │                                   │
                        │  Translator (FC/Thinking adaptation)      │
                        │       │                                   │
                        │  Source Manager ──> [Sources...]          │
                        │       │                                   │
                        │  SQLite (logs/stats)                      │
                        └──────────────────────────────────────────┘
```

**Key modules in `internal/`:**
- `api/proxy.go` - OpenAI-compatible proxy endpoints (`/v1/chat/completions`, `/v1/models`)
- `api/admin.go` - Management API (`/api/sources`, `/api/logs`, `/api/stats`)
- `api/middleware.go` - Auth, CORS, Recovery, Logger middlewares + static file serving
- `core/router.go` - Routing strategies: priority, round-robin, weighted, least-latency
- `core/translator.go` - Request/response format conversion, FC/Thinking passthrough, CPA adaptation
- `core/health.go` - Source health monitoring with state machine + CPA model/provider auto-detection
- `core/source.go` - Multi-source management with capability-based filtering
- `model/source.go` - Source data model, CPA provider capability matrix, thread-safe status
- `model/request.go` - OpenAI-compatible ChatCompletionRequest/Response with FC, Thinking, Vision
- `model/log.go` - RequestLog, DailyStats, SourceStats models
- `store/sqlite.go` - SQLite CRUD with incremental migration
- `config/config.go` - YAML config loading with defaults

## API Endpoints

**Proxy (OpenAI-compatible):**
- `POST /v1/chat/completions` - Chat completions with streaming support
- `GET /v1/models` - List available models across all sources

**Admin:**
- `GET/POST /api/sources` - List/add sources
- `GET/PUT/DELETE /api/sources/:id` - Source CRUD
- `POST /api/sources/:id/test` - Test source connection
- `GET /api/sources/:id/balance` - Query source balance (NewAPI only)
- `GET /api/status` - System status overview
- `GET /api/health` - All sources health status
- `GET /api/logs` - Request logs (supports source_id, model, success, limit, offset params)
- `GET /api/stats` - Usage statistics (daily + per-source)
- `GET/PUT /api/config` - Configuration management

Auth notes:
- `/v1/*` uses `server.api_key` (optional; empty means no auth)
- `/api/*` uses `server.admin_api_key` (optional; empty means no auth)

## Source Types

| Type | Identifier | Description |
|------|------------|-------------|
| NewAPI | `newapi` | one-api/new-api relay sites |
| CPA | `cpa` | CLIProxyAPI reverse proxy |
| OpenAI | `openai` | OpenAI official API |
| Anthropic | `anthropic` | Claude official API |
| Custom | `custom` | Any OpenAI-compatible API |

## CPA (CLIProxyAPI) Support

CPA sources have special protocol adaptation:

- **No Extended Thinking**: CPA never supports Thinking; it's stripped in Translator and filtered in Router
- **FC by Provider**: Function Calling support depends on the underlying provider, not the CPA source itself
- **Auto-detection**: Health checker probes `/v1/models` to discover models and their providers
- **Optional auth**: API key is optional; if empty, no Authorization header is sent
- **Provider/account-mode aware**: `cpa.providers` and `cpa.account_mode` affect effective provider/model eligibility

**Provider capability matrix** (defined in `model/source.go`):

| Provider | FC | Vision |
|----------|:--:|:------:|
| gemini   | ✓  | ✓      |
| claude   | ✓  | ✓      |
| codex    | ✓  | ✓      |
| qwen     | ✗  | ✓      |

**Key methods:**
- `Source.SupportsFCForModel(model)` - Checks if CPA provider for a given model supports FC
- `Source.GetProviderForModel(model)` - Looks up provider from runtime-detected ModelProviders map
- `HealthChecker.detectCPAModels()` - Probes `/v1/models` and builds model→provider mapping

## Key Design Decisions

1. **FC/Thinking Passthrough + Degrade**: Tools and extended_thinking params are forwarded to capable sources; when no FC-capable source exists, router falls back to non-FC source and translator degrades tools out of the request
2. **Failover**: On source failure, automatically retry with next healthy source (max 2 retries by default)
3. **Streaming**: Full SSE streaming support for `/v1/chat/completions`
4. **Config**: Sources can be configured via `config.yaml` or Web UI; `/api/config` updates routing/health_check/logging in-memory and persists to `config.yaml`
5. **CPA Adaptation**: CPA sources are treated specially in Router (capability filtering), Translator (request transformation), and HealthChecker (auto-detection)
6. **Thread Safety**: Source status uses sync.RWMutex; SourceManager uses RWMutex for the sources map
7. **Incremental Migration**: SQLite schema uses `CREATE TABLE IF NOT EXISTS` + `ALTER TABLE ADD COLUMN` for backwards compatibility

## Routing Strategies

| Strategy | Description |
|----------|-------------|
| `priority` | Select by priority, round-robin if equal (default) |
| `round-robin` | Rotate through all healthy sources |
| `weighted` | Distribute traffic by weight |
| `least-latency` | Select lowest latency source |
| `least-cost` | Select highest balance/cheapest source |

## Source Capabilities

Each source declares its capabilities:
- `function_calling` - Supports OpenAI-style tools/functions
- `extended_thinking` - Supports Claude extended thinking
- `vision` - Supports image inputs
- `models` - List of supported model names (empty = supports all)

## Frontend

Vue3 SPA in `web/`:
- Built with Vite, output to `dist/web/`
- Backend serves static files from `dist/web/` with SPA fallback
- Dev mode: `npm run dev` with proxy to backend on `:8080`
- Pages: Dashboard, Sources, Logs, Settings
- Components: SourceCard (display), SourceForm (create/edit with CPA-specific fields)
