# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

FusionAPI is a lightweight AI API aggregation gateway written in Go with a Vue3 Web UI. It unifies multiple AI API sources (NewAPI, CPA, OpenAI, Anthropic, Custom) behind a single OpenAI-compatible endpoint with intelligent routing, health checking, failover, FC degradation fallback, and multi-key management.

## Build & Run Commands

```bash
# Backend (Go)
go build -o fusionapi ./cmd/fusionapi
go run ./cmd/fusionapi
go test ./...                         # all tests
go test -v ./internal/core/...        # specific package
go test -run TestRouter ./...         # specific test

# Frontend (Vue3)
cd web && npm install
cd web && npm run dev                 # dev server on :3000, proxies to :18080
cd web && npm run build               # production build to dist/web/

# Docker
docker-compose up -d
```

## Architecture

```
Client ──> /v1/* ──> AuthMiddleware (multi-key + rate limit + tool detect)
                         │
                    ProxyHandler ──> Router ──> Source (with failover)
                         │               │
                    Translator      HealthChecker
                         │
                    SQLite (logs)

Admin UI ──> /api/* ──> AdminAuthMiddleware ──> AdminHandler
```

### Request Flow (Proxy)

1. `AuthMiddleware` — extracts Bearer token, looks up `api_keys` table first, falls back to `server.api_key`. Detects calling tool from headers. Enforces rate limits (RPM/daily/concurrent).
2. `ProxyHandler.ChatCompletions` — parses OpenAI-format request, enters retry loop.
3. `Router.RouteRequest` — selects healthy source by strategy, filtering by model/FC/Thinking capabilities. Excludes already-tried sources for failover.
4. `Translator.TranslateRequest` — adapts request for source type (CPA stripping, FC passthrough).
5. If source lacks FC but request has tools, `handleFCCompatRequest` degrades to prompt-based FC simulation.
6. Response streamed (SSE) or returned as JSON. Logged with client info.

### Key Modules (`internal/`)

| Module | Purpose |
|--------|---------|
| `api/proxy.go` | `/v1/chat/completions`, `/v1/models` proxy endpoints |
| `api/fc_compat.go` | FC degradation: prompt-based tool calling for non-FC sources |
| `api/admin.go` | Management API: sources CRUD, keys CRUD, logs, stats, config |
| `api/middleware.go` | Auth (multi-key + legacy), CORS, Recovery, Logger, route setup |
| `core/router.go` | Routing strategies: priority, round-robin, weighted, least-latency, least-cost |
| `core/translator.go` | Request/response format conversion, CPA adaptation |
| `core/health.go` | Source health monitoring, CPA model/provider auto-detection |
| `core/source.go` | SourceManager: thread-safe source map with store persistence |
| `core/ratelimit.go` | In-memory rate limiter: RPM sliding window, daily quota, concurrent count |
| `core/tooldetect.go` | HTTP header-based client tool identification |
| `model/source.go` | Source model, CPA provider capability matrix, thread-safe status |
| `model/apikey.go` | APIKey, KeyLimits, ClientInfo, ToolStats models |
| `model/request.go` | OpenAI-compatible request/response types with FC, Thinking, Vision |
| `model/log.go` | RequestLog (with client_ip/tool/key_id), stats models, LogQuery |
| `store/sqlite.go` | SQLite CRUD with incremental migration (sources, request_logs, api_keys) |
| `config/config.go` | YAML config with defaults, "auto" key generation |

### Frontend (`web/src/`)

Vue3 SPA with Vite + Pinia + TypeScript.

| File | Purpose |
|------|---------|
| `api/index.ts` | Typed API client with 401 auto-retry, all endpoint definitions |
| `stores/source.ts` | Pinia store for sources CRUD |
| `stores/apikey.ts` | Pinia store for API keys CRUD |
| `views/Dashboard.vue` | Status cards, source health, tool distribution, recent logs |
| `views/Sources.vue` | Source management with SourceForm/SourceCard components |
| `views/ApiKeys.vue` | Key management: create/edit/block/rotate/delete with masked display |
| `views/Logs.vue` | Request logs with tool/key columns and filters |
| `views/Cpa.vue` | CPA reverse proxy management |
| `views/Settings.vue` | Runtime config editor |

## API Endpoints

**Proxy (OpenAI-compatible, auth: `server.api_key` or managed keys):**
- `POST /v1/chat/completions` — streaming and non-streaming
- `GET /v1/models`

**Admin (auth: `server.admin_api_key`):**
- Sources: `GET/POST /api/sources`, `GET/PUT/DELETE /api/sources/:id`, `POST /api/sources/:id/test`, `GET /api/sources/:id/balance`
- Keys: `GET/POST /api/keys`, `GET/PUT/DELETE /api/keys/:id`, `POST /api/keys/:id/rotate`, `PUT /api/keys/:id/block`, `PUT /api/keys/:id/unblock`
- Stats: `GET /api/status`, `GET /api/health`, `GET /api/logs`, `GET /api/stats`, `GET /api/tools/stats`
- Config: `GET/PUT /api/config`

## Auth System

Two layers:
- `/v1/*` — `AuthMiddleware`: checks `api_keys` table first (with rate limits, tool whitelist, enabled check), then falls back to `server.api_key`. `ClientInfo` (key_id, tool, ip) stored in gin.Context.
- `/api/*` — `AdminAuthMiddleware`: simple single-key check against `server.admin_api_key`.

Empty key value = no auth for that scope. `"auto"` = generated at startup and persisted.

## Key Design Patterns

1. **Store CRUD pattern**: JSON-marshal nested structs (KeyLimits, Capabilities) into SQLite TEXT columns. Use `COALESCE` for nullable columns in SELECT. Incremental migration via `ALTER TABLE ADD COLUMN` (errors ignored for idempotency).
2. **ClientInfo threading**: `AuthMiddleware` sets `client_info` in gin.Context. `ProxyHandler` extracts it and passes through handler chain to `logRequest`/`logStreamRequest`.
3. **FC Degrade**: When request has tools but source lacks FC, `fc_compat.go` builds a system prompt with tool schemas, strips tool fields, then parses JSON response back into tool_call format.
4. **Thread safety**: `Source.Status` uses `sync.RWMutex` via `GetStatus()`/`SetStatus()`. `SourceManager.sources` map protected by `sync.RWMutex`.
5. **Frontend API pattern**: `request<T>()` wrapper auto-attaches admin key from localStorage, auto-prompts on 401. Each API object (sourcesApi, keysApi, etc.) follows same shape.

## CPA Special Handling

CPA sources require provider-aware capability checking:
- FC support depends on provider (gemini/claude/codex=yes, qwen=no), not the CPA source itself
- `Source.SupportsFCForModel(model)` looks up runtime-detected `ModelProviders` map
- Health checker probes `/v1/models` to build model→provider mapping
- Extended Thinking is always stripped for CPA sources
- `cpa.providers` and `cpa.account_mode` affect routing eligibility

## Source Types

| Type | Identifier | Notes |
|------|------------|-------|
| NewAPI | `newapi` | one-api/new-api relay, supports balance check |
| CPA | `cpa` | CLIProxyAPI, provider-aware FC, optional auth |
| OpenAI | `openai` | Standard OpenAI API |
| Anthropic | `anthropic` | Uses x-api-key header |
| Custom | `custom` | Any OpenAI-compatible endpoint |
