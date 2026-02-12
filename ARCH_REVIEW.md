# FusionAPI 架构 Review（Go + Vue3 AI API 聚合网关）

> Review 范围：后端 Go（`cmd/` + `internal/`）为主，重点覆盖：架构/模块划分、设计模式改进空间、错误处理与边界、并发安全、配置管理、CPA 适配层与 FC 兼容层。
>
> 结论概览：整体模块划分清晰（api / core / model / store / config），功能闭环完整（路由/健康检查/Failover/多 Key/FC 兼容/管理 API）。但当前实现里存在几处 **P0 级别的并发数据竞争与限流逻辑缺陷**，以及 **上游错误可观测性不足**。建议优先修复 P0，再做 P1 的架构抽象与可测试性改造。

---

## 0. 架构总览（现状评估）

当前后端分层：

- `internal/api/`：Gin 路由与 Handler（proxy/admin/middleware）
- `internal/core/`：路由策略、源管理、健康检查、限流、工具识别、翻译器
- `internal/model/`：请求/响应模型、Source/Key/Log 等数据结构
- `internal/store/`：SQLite CRUD + 迁移
- `internal/config/`：YAML 配置加载/保存 + 默认值 + auto key

请求流：

1. `/v1/*` -> `AuthMiddleware`（多 key、工具识别、限流）
2. `ProxyHandler.ChatCompletions` -> `Router.RouteRequest` -> `Translator.TranslateRequest` -> upstream
3. 若无原生 FC 能力 -> `fc_compat` 兼容层（prompt 注入 + JSON 解析回 tool_calls）
4. 记录日志到 SQLite

这套链路基本合理；`core` 中 Router/Health/RateLimiter/Translator 分工明确。

---

## 1) P0（必须优先修复）：并发安全与正确性问题

### P0.1 RateLimiter 的并发限制（Concurrent）目前**基本无效**

**现状**：
- 并发限制检查发生在 `AuthMiddleware -> rateLimiter.AllowWithTool -> Allow()` 里（读取 `r.concurrent[keyID]`）。
- 实际并发计数的 `AcquireConcurrent/ReleaseConcurrent` 却在 `ProxyHandler.ChatCompletions` 中调用，且在通过 `AllowWithTool` 之后才递增。

**后果**：
- 多个请求可以同时通过并发检查（因为计数还没 +1），导致并发上限被轻易突破。

**建议（推荐方案）**：将“检查 + 占用并发令牌 + 释放”统一放进 RateLimiter 的同一个临界区，并且放在 **middleware** 层执行（这样能在进入 handler 前就拒绝）。

示例实现：

```go
// core/ratelimit.go
func (r *RateLimiter) Enter(keyID string, limits model.KeyLimits, tool string) (release func(), allowed bool, reason string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    now := time.Now()

    // 1) auto-ban
    if banTime, ok := r.autoBanned[keyID]; ok {
        if time.Since(banTime) < AutoBanDuration {
            return nil, false, "API key is temporarily auto-banned"
        }
        delete(r.autoBanned, keyID)
        delete(r.errorCount, keyID)
    }

    // 2) RPM / DailyQuota（略：直接内联原 Allow 逻辑，避免多次 Lock）

    // 3) Concurrent：检查 + 占用
    if limits.Concurrent > 0 {
        if r.concurrent[keyID] >= limits.Concurrent {
            return nil, false, fmt.Sprintf("Concurrent limit exceeded (%d/%d)", r.concurrent[keyID], limits.Concurrent)
        }
        r.concurrent[keyID]++
    }

    // 4) 记录 RPM / daily

    // 5) tool quota

    // release closure
    return func() {
        r.mu.Lock()
        if r.concurrent[keyID] > 0 {
            r.concurrent[keyID]--
        }
        r.mu.Unlock()
    }, true, ""
}
```

Gin middleware 侧使用：

```go
release, allowed, reason := rateLimiter.Enter(apiKeyObj.ID, apiKeyObj.Limits, tool)
if !allowed { /* 429 */ }
if release != nil { defer release() }
```

同时删除/废弃 `ProxyHandler.ChatCompletions` 里的 `AcquireConcurrent/ReleaseConcurrent`，避免双计数。

---

### P0.2 存在多个**数据竞争（data race）**点：Source.Capabilities、Source.Status、Config、Router.strategy

#### (1) `HealthChecker.detectCPAModels()` 在并发协程中直接修改 `src.Capabilities`（无锁）

**代码位置**：`internal/core/health.go`：

- `detectCPAModels` 里：
  - `src.Capabilities.Models = detectedModels`
  - `src.Capabilities.FunctionCalling = hasFC`
  - `src.Capabilities.Vision = hasVision`
  - `src.Capabilities.ExtendedThinking = false`

而 Router/SourceManager 在路由选择时会读取 `src.Capabilities`（无锁）。

**后果**：在 `-race` 下会报数据竞争；更严重时可能导致不可预测路由行为。

**建议**：把“运行期动态探测能力”从 `Capabilities`（配置属性）分离出来，作为 **runtime capability** 存在 `SourceStatus` 或单独 runtime struct，并通过锁保护。

推荐结构：

- `Source`：配置字段（BaseURL/APIKey/Enabled/Weight/Priority/CPAConfig）
- `SourceRuntime`（或放入 Status）：动态字段（Latency/State/Balance/ModelProviders/DetectedModels/DetectedCaps）

最小改动方案：在 `model.Source` 上提供线程安全的 `GetCapabilities/SetCapabilities`（或复用现有 `mu` 保护 capabilities + status）。

例如：

```go
// model/source.go
func (s *Source) GetCapabilities() Capabilities {
    s.mu.RLock(); defer s.mu.RUnlock()
    return s.Capabilities
}

func (s *Source) SetCapabilities(c Capabilities) {
    s.mu.Lock(); defer s.mu.Unlock()
    s.Capabilities = c
}
```

然后所有读取都改为 `GetCapabilities()`。

#### (2) `GetProviderForModel` 直接读 `s.Status` 指针（无锁）

**代码位置**：`internal/model/source.go`：

```go
func (s *Source) GetProviderForModel(modelName string) string {
    if s.Status != nil && s.Status.ModelProviders != nil { ... }
}
```

但 Status 的写入通过 `SetStatus` 持锁进行。该处绕过锁读取会 data race。

**建议**：统一通过 `GetStatus()` 读取，并注意 map 的深拷贝。

#### (3) `GetStatus()` 返回的是浅拷贝，`ModelProviders`（map）未深拷贝

当前 `GetStatus()`：

```go
status := *s.Status
return &status
```

`status.ModelProviders` 仍然指向同一个 map。若未来有增量更新 map 的逻辑，会立刻触发 data race。

**建议**：对 map 做 deep copy：

```go
if status.ModelProviders != nil {
    mp := make(map[string]string, len(status.ModelProviders))
    for k, v := range status.ModelProviders { mp[k] = v }
    status.ModelProviders = mp
}
```

#### (4) `AdminHandler.UpdateConfig` 修改 `h.cfg`，但 ProxyHandler/Router/HealthChecker 并发读取无锁

- `AdminHandler` 用 `cfgMu` 保护写
- 其他 goroutine 读取 `h.cfg`（例如 ProxyHandler 内的 failover 配置）不加锁

同理：`Router.SetStrategy` 写 `r.strategy`，`RouteRequest` 读 `r.strategy`，无锁。

**建议**：
- 用 `atomic.Value` 存储不可变配置快照（更新时整体替换）
- Router 的 `strategy` 用 `atomic.Value` 或 `sync.RWMutex`

示例：

```go
type ConfigHolder struct{ v atomic.Value } // stores *config.Config
func (h *ConfigHolder) Get() *config.Config { return h.v.Load().(*config.Config) }
func (h *ConfigHolder) Set(c *config.Config) { h.v.Store(c) }
```

管理 API 更新配置时：构造新 cfg（深拷贝/复制），校验后 `holder.Set(newCfg)`。

---

### P0.3 流式请求（SSE）在错误/断流场景下的日志与状态更新不完整

**现状**：`handleStreamRequest` 中：

- 读取 SSE 过程中若 `ReadString` 返回非 EOF error，直接 `return true`（表示“已开始输出，不能 failover”），但：
  - 不会记录失败日志
  - 不会更新 source latency/error count

**后果**：
- 统计与 auto-ban 依赖日志 success/error，会出现“明明失败但记录成功/或根本没记录”的情况
- 源健康状态可能长期不准确

**建议**：
- 对 stream handler 使用 `defer` 统一收尾：记录 log + 更新 latency
- 如果出现读错误：log 标记 `success=false`，并调用 `updateSourceLatency(..., err)`

---

## 2) P1（高优先级）：错误处理、边界条件与可观测性

### P1.1 上游错误信息被吞掉，导致排障困难

在 `handleNormalRequest/handleStreamRequest` 中，上游返回非 200 时，仅 `return false`，外层把 lastError 设成 `source X failed`。

**建议**：
- 对非 200 响应：读取 `resp.Body`（限长），把 body 片段写入错误并记录到 `RequestLog.Error`
- failover 过程中保留“最后一次失败原因”（包括 status code 与 body）

示例：

```go
b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
err := fmt.Errorf("upstream status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(b)))
```

### P1.2 每次尝试（attempt）的 latency 统计混用“总耗时”

`updateSourceLatency(src, time.Since(startTime), ...)` 使用的是整次请求起始时间，而不是“当前源尝试”的起始时间。

**后果**：
- 第 2 次/第 3 次 failover 的源 latency 会被前面尝试的时间污染

**建议**：在每个 attempt 内部用 `attemptStart := time.Now()`，上游请求完成后用 `time.Since(attemptStart)` 更新该源 latency。

### P1.3 CORS 允许头不包含 `X-Client-Name`

`CORSMiddleware`：

```go
Access-Control-Allow-Headers: Content-Type, Authorization, X-Requested-With
```

但工具识别支持 `X-Client-Name`。浏览器环境下会被 CORS 拦。

**建议**：加入 `X-Client-Name`（以及可能的 `X-Client-Version` 等）。

### P1.4 Source 更新 APIKey 的语义不清：无法“显式清空”

`AdminHandler.UpdateSource`：如果请求里 `api_key==""` 会保留旧 key。

**后果**：
- 无法通过 API/UI 清空 APIKey（例如 CPA 允许空 key）

**建议**：
- 用指针字段 `*string` 区分“未提供”与“提供空字符串”
- 或增加显式字段：`api_key_present` / `clear_api_key: true`

### P1.5 SQLite 迁移中忽略错误：可能掩盖真实问题

`migrate()` 多处 `s.db.Exec("ALTER TABLE ...")` 没有检查错误。

**建议**：至少判断并忽略“duplicate column name”类的可预期错误，其它错误应返回/记录。

---

## 3) P2（中优先级）：架构与设计模式改进（接口抽象、依赖注入、可测试性）

### P2.1 引入接口边界（Repository / Service）提升可测试性与可演进性

目前 `api` 直接依赖 `*store.Store`、`*core.SourceManager`、`*core.Router` 等具体实现，单元测试需要真实 SQLite 或较多桩。

**建议**：抽象出最小接口：

- `SourceRepository`：Save/List/Delete
- `LogRepository`：Save/Query/Stats
- `KeyRepository`：GetByKey/Save/List
- `Router`：RouteRequest

示例：

```go
type KeyStore interface {
    GetAPIKeyByKey(string) (*model.APIKey, error)
    UpdateAPIKeyLastUsed(string) error
}
```

这样可在测试中使用 in-memory fake，实现 Router/Health/Proxy 的可测。

### P2.2 把“能力判断”抽成策略对象（CapabilityPolicy）

当前 capability 分散在：
- `SourceManager.GetByCapability`
- `model.Source.SupportsFCForModel`
- `Translator.TranslateRequest`（CPA thinking stripping）
- `fc_compat.sourceSupportsFC`

**建议**：集中为一个 policy：

- 输入：`req` + `src` +（可能的 runtime detected info）
- 输出：`eligible`、`needsFCCompat`、`translatedReq`（或 translation plan）

好处：
- CPA/FC 兼容逻辑不再散落多处
- 新增 provider 或新增能力（比如 JSON schema、reasoning 模式）更简单

### P2.3 将 Failover 作为显式组件，统一重试/退避/熔断

目前 failover 在 ProxyHandler 内用 for loop 实现。

**建议**：抽出 `FailoverExecutor`：

- 统一“排除已尝试源”
- 可加入 backoff、可配置只对特定错误重试、或熔断策略

---

## 4) 配置管理（现状与改进）

### 现状优点
- YAML 配置 + defaults + `auto` key 生成并落盘，易用。
- 管理 API 支持 runtime 更新部分配置，并提示 `restart_required`。

### 主要问题
- **并发读写无锁（P0.2）**：更新配置时可能 data race。
- `config.Get()` 全局变量存在但实际没形成“单一真相”。

### 建议
1. 用 `atomic.Value` 存 `*Config` 快照，作为全局/注入的配置读取入口。
2. UpdateConfig 时基于旧快照拷贝生成新快照，校验通过后一次性替换。
3. 明确哪些配置可热更新、哪些必须重启；把可热更新配置分组。

---

## 5) CPA 适配层与 FC 兼容层：架构合理性评估

### 5.1 CPA 适配层（优点）

当前 CPA 的关键点实现得比较到位：
- Router 在候选过滤中体现 CPA 约束：
  - Thinking 不支持（直接排除）
  - FC 能力依赖 provider（`SupportsFCForModel` + provider enable list）
- HealthChecker 支持 `auto_detect`：通过 `/v1/models` 生成 `model -> provider` 映射，并动态更新模型列表与能力（FC/Vision）。

**总体评价**：方向正确：把“CPA 是聚合上游”这一事实编码进 capability 判断。

### 5.2 主要结构性问题：动态能力写入 `src.Capabilities` 容易造成竞态与语义混淆

- `Capabilities` 从语义上更像“静态声明/配置”，但 CPA auto-detect 让它变成“动态变化”。

**建议**：
- 保留 `Capabilities` 作为静态配置（可选覆盖）
- 新增 `DetectedCapabilities`/`DetectedModels`（runtime）
- 路由选择优先使用 runtime detected（如果存在），fallback 到静态声明

### 5.3 FC 兼容层（fc_compat）评估

**优点**：
- 兼容层实现闭环：工具 schema 注入 system prompt + 上游返回 JSON + 网关转回 OpenAI `tool_calls`。
- 对 streaming 请求给出“伪流式”输出，客户端兼容性更高。

**风险/边界**：
- 依赖模型严格输出 JSON；当模型漂移时会回退为普通文本，可能让客户端误判。
- 目前未做 schema 级校验/重试策略。

**建议（增强鲁棒性）**：
1. 为 compat 输出增加 JSON schema 校验（至少校验字段存在/类型正确）。
2. 解析失败时可加一次“纠错重试”（把解析错误反馈回模型，要求只输出 JSON）。
3. 日志增加字段：`compat_parse_ok`、`compat_tool_name`，便于观察质量。

---

## 6) 其它建议（P3 / 优化项）

- **Graceful shutdown**：`cmd/fusionapi/main.go` 目前 `os.Exit(0)`，建议使用 `http.Server` + `Shutdown(ctx)`。
- **日志清理**：配置有 `logging.retention_days`，store 有 `CleanOldLogs`，但未看到定时执行；建议在后台 goroutine 定期清理。
- **HTTP client**：可为每个 source 定制 transport（连接池、TLS、代理等），并限制最大并发连接。

---

## 7) 建议的修复顺序（行动清单）

1. **P0**：修复 RateLimiter 并发限制的原子性与生效位置（middleware）
2. **P0**：修复 data race：
   - Source.Status/ModelProviders 访问统一走锁 + deep copy
   - CPA auto-detect 不再无锁写 Capabilities（或加锁/移入 runtime）
   - Config/Router.strategy 用 atomic 或 RWMutex
3. **P0/P1**：完善 stream 断流错误日志与健康统计
4. **P1**：保留/记录 upstream 错误细节，优化可观测性
5. **P2**：抽象 repo/service 接口，集中 capability policy，降低耦合、提升可测

---

### 附：可快速验证的检查项

- `go test ./... -race`：应能捕获上述 data race（建议加一些并发路由/健康检查的测试）
- 压测并发限制：同一 key 设置 `concurrent=1`，并发 5 个请求应有 4 个立即 429
- CPA auto-detect 开启后：连续路由 + 健康检查同时运行不应出现 race
