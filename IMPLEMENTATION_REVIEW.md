# FusionAPI 实现梳理（2026-02-07）

> 本文用于快速说明“当前已实现什么、怎么实现、还有什么边界”。  
> 重点：管理 API、CPA 反代、第三方 API 不支持 FC 的处理策略。

---

## 1. 当前实现总览

后端（Go）：
- 代理 API：
  - `POST /v1/chat/completions`
  - `GET /v1/models`
- 管理 API：
  - `sources/status/health/logs/stats/config` 全套接口已实现
- 路由策略：
  - `priority` / `round-robin` / `weighted` / `least-latency` / `least-cost`
- Failover：
  - 源失败后按配置自动切换重试
- 健康检查：
  - 周期探测 `/v1/models`，维护 `healthy/unhealthy`
- 存储：
  - SQLite 保存源配置、请求日志与统计
- 多 Key 认证：
  - api_keys 表优先匹配
  - fallback 到 server.api_key
- 频率限制：
  - RPM 滑动窗口
  - 日配额计数
  - 并发数控制
- 工具识别：
  - 从 HTTP Header 识别调用工具
  - 支持 cursor/claude-code/codex-cli/continue/copilot 等

前端（Vue3）：
- 页面：
  - Dashboard / Sources / CPA / Logs / Settings / API Keys
- 支持在 Settings 修改 `server.host/server.port`，并提示 `restart_required`
- 支持在 Sources 配置 CPA 专属参数（providers/account_mode/auto_detect）
- API Keys 页面：Key 的创建/编辑/封禁/轮换/删除
- Dashboard 增加工具分布统计卡片
- Logs 页面增加工具和 Key 列及筛选

默认端口：
- `18080`

---

## 2. 管理 API 实现情况

路由注册位置：`internal/api/middleware.go`

已实现接口：
- 源管理：
  - `GET /api/sources`
  - `POST /api/sources`
  - `GET /api/sources/:id`
  - `PUT /api/sources/:id`
  - `DELETE /api/sources/:id`
  - `POST /api/sources/:id/test`
  - `GET /api/sources/:id/balance`
- Key 管理：
  - `GET /api/keys`
  - `POST /api/keys`
  - `GET /api/keys/:id`
  - `PUT /api/keys/:id`
  - `DELETE /api/keys/:id`
  - `POST /api/keys/:id/rotate`
  - `PUT /api/keys/:id/block`
  - `PUT /api/keys/:id/unblock`
- 状态：
  - `GET /api/status`
  - `GET /api/health`
- 日志统计：
  - `GET /api/logs`
  - `GET /api/stats`
- 工具统计：
  - `GET /api/tools/stats`
- 配置：
  - `GET /api/config`
  - `PUT /api/config`

`/api/config` 当前支持更新：
- `server.host`
- `server.port`（限制 `1024-65535`）
- `routing`
- `health_check`
- `logging`

当 `host/port` 改变时，返回：
- `restart_required: true`

鉴权：
- `/api/*` 由 `server.admin_api_key` 控制（可选）
- `/v1/*` 由 `server.api_key` 控制（可选）
- 两者支持 `"auto"` 自动生成

---

## 3. CPA 反代实现情况（你关心的重点）

### 3.1 数据结构与配置

CPA 作为一种独立源类型：`type = "cpa"`

CPA 专属配置字段：
- `cpa.providers`
- `cpa.account_mode`（`single` 或 `multi`）
- `cpa.auto_detect`

Provider 能力矩阵（内置）：
- `gemini`: FC=true, Vision=true
- `claude`: FC=true, Vision=true
- `codex`: FC=true, Vision=true
- `qwen`: FC=false, Vision=true

### 3.2 健康检查与自动探测

HealthChecker 对 CPA 的行为：
- 常规探测：`GET {base_url}/v1/models`
- 若 `auto_detect=true`：
  - 解析 models 响应中的 `provider`
  - 建立 `model -> provider` 映射
  - 按探测结果动态更新该源能力（FC/Vision）
  - Thinking 固定不支持

### 3.3 路由层面的 CPA 规则

在候选源筛选时：
- CPA 不支持 Thinking，请求含 thinking 则 CPA 直接排除
- 请求含 tools 时，CPA 是否可用取决于“该模型对应 provider 是否支持 FC”
- 若模型已映射 provider，还会检查 provider 是否在当前 CPA 配置中启用

### 3.4 前端可见性

已提供独立 `CPA` 页面（`/cpa`）：
- 展示统一反代入口：`{origin}/v1`
- 展示已配置 CPA 源（上游地址/provider/模式/状态）
- 可快速跳转到 Sources 继续管理

---

## 4. 第三方 API 不支持 FC 时如何处理（核心问题）

当前实现是“能力筛选 + FC 兼容层”两层兜底：

### 4.1 第一层：优先选支持 FC 的源

Router 收到含 tools 的请求时，会先找 `needFC=true` 的候选源。  
如果能找到支持 FC 的源，走正常透传。

### 4.2 第二层：找不到 FC 源则回退到非 FC 源

若没有 FC 候选，Router 会显式允许回退：
- 再次筛选 `needFC=false` 的候选源
- 保持请求不中断，继续可用性优先

### 4.3 第三层：兼容层执行协议转换

当目标源本身不支持 FC 时，网关会启用兼容层：
- 把工具定义注入系统提示词，要求模型只返回结构化 JSON。
- 不支持 FC 的上游仍使用“清空工具字段后的请求”执行，避免上游报错。
- 网关把上游 JSON 结果映射回 OpenAI 标准响应：
  - `{"tool_call": ...}` -> `assistant.tool_calls` + `finish_reason=tool_calls`
  - `{"final": ...}` -> 普通 assistant 文本

对于 CPA：
- 先移除 thinking
- 再按 provider 能力决定是否移除 FC 字段

### 4.4 结果与边界

优点：
- 不会因为“上游不支持 FC”导致整次请求硬失败
- 在混合多源环境下，服务稳定性更高
- 客户端仍可按 OpenAI tool calling 协议继续下一轮

边界：
- 兼容层依赖模型按约定输出 JSON；当模型漂移输出格式时，会回退为普通文本响应
- 复杂多工具并行场景可继续增强（当前以“单次决策/单轮返回”为主）

---

## 5. API Key 管理实现情况

### 5.1 数据模型

位置：`internal/model/apikey.go`

```go
type APIKey struct {
    ID           string
    Key          string      // sk-fa-xxx
    Name         string
    Enabled      bool
    Limits       KeyLimits
    AllowedTools []string
    CreatedAt    time.Time
    LastUsedAt   time.Time
}

type KeyLimits struct {
    RPM        int  // 每分钟请求数
    DailyQuota int  // 每日配额
    Concurrent int  // 并发数
}

type ClientInfo struct {
    KeyID string
    Tool  string
    IP    string
}
```

### 5.2 认证流程

位置：`internal/api/middleware.go`

AuthMiddleware 处理流程：
1. 工具识别（DetectTool）
2. 提取 Bearer token
3. 查询 api_keys 表匹配
4. 若匹配：检查 enabled、allowed_tools、rate limits
5. 若不匹配：fallback 到 server.api_key
6. 将 ClientInfo 存入 gin.Context

### 5.3 频率限制

位置：`internal/core/ratelimit.go`

RateLimiter 实现：
- RPM：内存滑动窗口（map[keyID][]timestamp）
- 日配额：map[keyID+date]count
- 并发：map[keyID]int（请求开始+1，结束-1）
- 后台协程定期清理过期数据

### 5.4 工具识别

位置：`internal/core/tooldetect.go`

识别规则：
- 优先检查 X-Client-Name 头
- 其次匹配 User-Agent 模式
- 支持：cursor, claude-code, codex-cli, continue, copilot, openai-sdk, anthropic-sdk

### 5.5 日志增强

request_logs 表新增列：
- client_ip
- client_tool
- api_key_id

前端展示：Logs 页面可按工具/Key 筛选

---

## 6. 当前未实现/预留项（与后续决策相关）

- `/v1/embeddings`：未实现（仅 chat/models 已上线）
- `HealthStateRemoved`：常量已定义，但未进入状态流转
- FC 兼容层可继续增强：更强 JSON 校验、并行多工具编排、流式细粒度事件
- 自动封禁规则：异常请求自动触发封禁
- Key 使用统计图表：历史趋势可视化
- 工具配额：按工具类型设置不同限制

---

## 7. 建议的下一步（按你当前目标）

1. 增强 FC 兼容层鲁棒性（高优先级）  
   增加严格 schema 校验、重试提示词、失败兜底策略，降低模型输出漂移风险。

2. 增加可观测性（高优先级）  
   在日志里显式记录 `fc_compat_used=true/false`、`compat_parse_ok`、`selected_source_type`。

3. CPA 侧增加可视化诊断（中优先级）  
   在 `/cpa` 页展示“模型->provider 映射探测结果”，排查 provider 配置错误会更快。
