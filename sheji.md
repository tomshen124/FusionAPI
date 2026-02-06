好，**FusionAPI** 就定了！

给你一份完整的设计文档：

---

# FusionAPI - 设计文档 v1.0

> 实现状态更新（2026-02-06）  
> 当前代码已落地：多源聚合、Failover、CPA Provider 自动探测、FC 降级兜底、配置热更新并持久化。  
> 同时支持可选 `admin_api_key`（保护 `/api/*`）和 `api_key`（保护 `/v1/*`），两者都支持填 `"auto"` 自动生成。

## 一、项目定位

**一句话**：轻量级 AI API 聚合网关，统一管理多个 API 源，提供智能路由、健康检查、完整 FC/Thinking 支持。

**目标用户**：个人开发者、小团队，需要管理多个 AI API 源的场景。

**核心价值**：
- 多源聚合，一个入口访问所有 API
- 自动故障切换，保证可用性
- 完整支持 FC + Extended Thinking
- 轻量部署，2+2 小机器也能跑

---

## 二、系统架构

```
                    ┌─────────────────────────────────┐
                    │         Web UI (Vue3)           │
                    │  仪表盘 │ 源管理 │ 日志 │ 设置  │
                    └────────────┬────────────────────┘
                                 │ REST API
┌────────────────────────────────▼────────────────────────────────┐
│                        FusionAPI Core (Go)                      │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                      API Server                           │  │
│  │            /v1/chat/completions (OpenAI 兼容)             │  │
│  │            /v1/models                                     │  │
│  └──────────────────────────┬───────────────────────────────┘  │
│                             │                                   │
│  ┌──────────────┐  ┌───────▼────────┐  ┌──────────────────┐   │
│  │   Health     │  │    Router      │  │    Translator    │   │
│  │   Checker    │◄─┤  (路由决策)    ├─►│  (格式转换)      │   │
│  │              │  │                │  │  FC/Thinking适配 │   │
│  └──────┬───────┘  └────────────────┘  └────────┬─────────┘   │
│         │                                        │             │
│  ┌──────▼────────────────────────────────────────▼──────────┐  │
│  │                   Source Manager                          │  │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐     │  │
│  │  │ NewAPI  │  │   CPA   │  │ OpenAI  │  │ Claude  │     │  │
│  │  │  站点A  │  │  反代   │  │  直连   │  │  直连   │     │  │
│  │  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘     │  │
│  └───────┼────────────┼────────────┼────────────┼───────────┘  │
│          │            │            │            │               │
│  ┌───────▼────────────▼────────────▼────────────▼───────────┐  │
│  │                    SQLite (日志/统计)                     │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 三、核心模块设计

### 3.1 Source Manager (源管理)

#### 支持的源类型

| 类型 | 标识 | 说明 |
|------|------|------|
| NewAPI | `newapi` | one-api/new-api 等中转站 |
| CPA | `cpa` | CLIProxyAPI 反代 |
| OpenAI | `openai` | OpenAI 官方 |
| Anthropic | `anthropic` | Claude 官方 |
| Custom | `custom` | 任意 OpenAI 兼容 API |

#### 源配置结构

```go
type Source struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Type        string            `json:"type"`        // newapi|cpa|openai|anthropic|custom
    BaseURL     string            `json:"base_url"`
    APIKey      string            `json:"api_key"`
    Priority    int               `json:"priority"`    // 数字越小优先级越高
    Weight      int               `json:"weight"`      // 负载均衡权重
    Enabled     bool              `json:"enabled"`
    
    // 能力声明
    Capabilities Capabilities     `json:"capabilities"`
    
    // 运行时状态
    Status      SourceStatus      `json:"-"`
}

type Capabilities struct {
    FunctionCalling  bool   `json:"function_calling"`
    ExtendedThinking bool   `json:"extended_thinking"`
    Vision           bool   `json:"vision"`
    Models           []string `json:"models"`  // 支持的模型列表
}

type SourceStatus struct {
    Healthy     bool
    Latency     time.Duration
    Balance     float64       // 余额
    LastCheck   time.Time
    ErrorCount  int
    LastError   string
}
```

---

### 3.2 Health Checker (健康检查)

#### 检查策略

```go
type HealthChecker struct {
    Interval    time.Duration  // 检查间隔，默认 60s
    Timeout     time.Duration  // 超时时间，默认 10s
    Threshold   int            // 连续失败多少次标记为不可用，默认 3
}
```

#### 检查内容

| 检查项 | 方式 | 频率 |
|--------|------|------|
| 可用性 | GET /v1/models | 每 60s |
| 延迟 | 记录响应时间 | 每次请求 |
| 余额 | 调用 NewAPI 余额接口 | 每 5min |

#### 健康状态机

```
         ┌─────────┐
         │ Healthy │◄──────────────┐
         └────┬────┘               │
              │ 连续失败 >= 3      │ 探测成功
              ▼                    │
         ┌─────────┐               │
         │Unhealthy│───────────────┘
         └─────────┘
              │ 持续失败 > 10min
              ▼
         ┌─────────┐
         │ Removed │ (从路由池移除，仅保留配置)
         └─────────┘
```

---

### 3.3 Router (路由决策)

#### 路由策略

| 策略 | 说明 |
|------|------|
| `priority` | 按优先级选择，优先级相同则轮询 |
| `round-robin` | 轮询所有健康源 |
| `weighted` | 按权重分配流量 |
| `least-latency` | 选择延迟最低的源 |
| `least-cost` | 选择余额最多/最便宜的源 |

#### 路由流程

```
请求进入
    │
    ▼
┌─────────────────────────────────┐
│ 1. 解析请求 (model, tools等)    │
└───────────────┬─────────────────┘
                │
                ▼
┌─────────────────────────────────┐
│ 2. 筛选候选源                    │
│    - 源 Enabled                 │
│    - 源 Healthy                 │
│    - 源支持该 model             │
│    - 如有 tools, 源支持 FC      │
│    - 如有 thinking, 源支持它    │
└───────────────┬─────────────────┘
                │
                ▼
┌─────────────────────────────────┐
│ 3. 按策略选择源                  │
└───────────────┬─────────────────┘
                │
                ▼
┌─────────────────────────────────┐
│ 4. 转发请求                      │
│    成功 → 返回                   │
│    失败 → Failover 到下一个源   │
└─────────────────────────────────┘
```

---

### 3.4 Translator (格式转换)

#### 核心职责

| 功能 | 说明 |
|------|------|
| **请求转换** | 统一入口格式 → 各源格式 |
| **响应转换** | 各源格式 → 统一出口格式 |
| **FC 适配** | 确保 tools/function_call 正确透传 |
| **Thinking 适配** | 确保 extended thinking 正确透传 |
| **流式处理** | SSE 流式响应正确转发 |

#### FC 透传逻辑

```go
func (t *Translator) TranslateRequest(req *ChatRequest, source *Source) *ChatRequest {
    // 如果请求包含 tools
    if len(req.Tools) > 0 {
        if !source.Capabilities.FunctionCalling {
            // 源不支持 FC，降级处理（可选：转成 prompt）
            return t.degradeFCToPrompt(req)
        }
        // 源支持 FC，直接透传
    }
    return req
}
```

#### Thinking 透传逻辑

```go
func (t *Translator) TranslateRequest(req *ChatRequest, source *Source) *ChatRequest {
    // 如果请求要求 thinking
    if req.Thinking != nil && req.Thinking.Enabled {
        if !source.Capabilities.ExtendedThinking {
            // 源不支持，移除 thinking 参数（或返回错误）
            req.Thinking = nil
        }
    }
    return req
}
```

---

### 3.5 数据模型

#### 请求日志

```go
type RequestLog struct {
    ID           string    `json:"id"`
    Timestamp    time.Time `json:"timestamp"`
    SourceID     string    `json:"source_id"`
    SourceName   string    `json:"source_name"`
    Model        string    `json:"model"`
    
    // 请求信息
    HasTools     bool      `json:"has_tools"`
    HasThinking  bool      `json:"has_thinking"`
    Stream       bool      `json:"stream"`
    
    // 响应信息
    Success      bool      `json:"success"`
    StatusCode   int       `json:"status_code"`
    Latency      int64     `json:"latency_ms"`
    
    // Token 统计
    PromptTokens     int   `json:"prompt_tokens"`
    CompletionTokens int   `json:"completion_tokens"`
    TotalTokens      int   `json:"total_tokens"`
    
    // 错误信息
    Error        string    `json:"error,omitempty"`
    
    // Failover 记录
    FailoverFrom string    `json:"failover_from,omitempty"`
}
```

#### 统计聚合

```go
type UsageStats struct {
    Date         string  `json:"date"`        // 2026-02-06
    SourceID     string  `json:"source_id"`
    Model        string  `json:"model"`
    
    RequestCount int     `json:"request_count"`
    SuccessCount int     `json:"success_count"`
    FailCount    int     `json:"fail_count"`
    
    TotalTokens  int64   `json:"total_tokens"`
    AvgLatency   float64 `json:"avg_latency_ms"`
}
```

---

## 四、API 设计

### 4.1 代理 API（对外）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/chat/completions` | POST | 聊天补全（核心） |
| `/v1/models` | GET | 列出所有可用模型 |
| `/v1/embeddings` | POST | 文本嵌入（可选） |

### 4.2 管理 API（内部）

| 端点 | 方法 | 说明 |
|------|------|------|
| **源管理** | | |
| `/api/sources` | GET | 列出所有源 |
| `/api/sources` | POST | 添加源 |
| `/api/sources/:id` | GET | 获取源详情 |
| `/api/sources/:id` | PUT | 更新源 |
| `/api/sources/:id` | DELETE | 删除源 |
| `/api/sources/:id/test` | POST | 测试源连接 |
| `/api/sources/:id/balance` | GET | 查询余额 |
| **状态** | | |
| `/api/status` | GET | 系统状态总览 |
| `/api/health` | GET | 各源健康状态 |
| **日志** | | |
| `/api/logs` | GET | 请求日志列表 |
| `/api/stats` | GET | 用量统计 |
| **配置** | | |
| `/api/config` | GET | 获取配置 |
| `/api/config` | PUT | 更新配置（routing/health_check/logging，热更新并持久化） |

> 认证说明：`/api/*` 支持可选 `server.admin_api_key`，配置后需携带 `Authorization: Bearer <admin_api_key>`。

---

## 五、Web UI 设计

### 5.1 页面结构

```
FusionAPI
├── 仪表盘 (Dashboard)
├── 源管理 (Sources)
├── 日志 (Logs)
└── 设置 (Settings)
```

### 5.2 仪表盘

```
┌─────────────────────────────────────────────────────────────┐
│  FusionAPI                              [设置] [文档]       │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │  活跃源     │  │  今日请求   │  │  成功率     │         │
│  │     5/7     │  │   12,345    │  │   99.2%     │         │
│  │   ● ● ● ○ ○ │  │   ↑ 15%    │  │             │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
│                                                             │
│  源状态                                                     │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 名称          类型      状态    延迟    余额        │   │
│  ├─────────────────────────────────────────────────────┤   │
│  │ 中转站A       newapi    ● 健康  120ms   $45.20     │   │
│  │ 我的CPA       cpa       ● 健康  45ms    -          │   │
│  │ OpenAI官方    openai    ● 健康  200ms   $12.00     │   │
│  │ 中转站B       newapi    ○ 离线  -       $0.50      │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  最近请求                                      [查看全部]   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 10:23:45  gpt-4  中转站A  ✓ 1.2s  FC:是           │   │
│  │ 10:23:40  claude 我的CPA  ✓ 0.8s  Thinking:是     │   │
│  │ 10:23:35  gpt-4  中转站B  ✗ Failover→中转站A      │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 5.3 源管理

```
┌─────────────────────────────────────────────────────────────┐
│  源管理                                    [+ 添加源]       │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ┌──────┐  中转站A                          [编辑]   │   │
│  │ │NewAPI│  https://api-a.example.com                 │   │
│  │ └──────┘  ● 健康 │ 延迟 120ms │ 余额 $45.20        │   │
│  │           FC ✓  Thinking ✓  Vision ✓               │   │
│  │           优先级: 1  │  权重: 100                   │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ┌──────┐  我的CPA                          [编辑]   │   │
│  │ │ CPA  │  http://localhost:8765                     │   │
│  │ └──────┘  ● 健康 │ 延迟 45ms │ 余额 -              │   │
│  │           FC ✓  Thinking ○  Vision ✓               │   │
│  │           优先级: 2  │  权重: 100                   │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 5.4 添加/编辑源弹窗

```
┌─────────────────────────────────────────┐
│  添加源                            [X]  │
├─────────────────────────────────────────┤
│                                         │
│  名称 *                                 │
│  ┌─────────────────────────────────┐   │
│  │ 中转站A                         │   │
│  └─────────────────────────────────┘   │
│                                         │
│  类型 *                                 │
│  ┌─────────────────────────────────┐   │
│  │ NewAPI                      ▼   │   │
│  └─────────────────────────────────┘   │
│                                         │
│  Base URL *                             │
│  ┌─────────────────────────────────┐   │
│  │ https://api.example.com         │   │
│  └─────────────────────────────────┘   │
│                                         │
│  API Key *                              │
│  ┌─────────────────────────────────┐   │
│  │ sk-xxxxxxxxxxxxxxxx             │   │
│  └─────────────────────────────────┘   │
│                                         │
│  优先级        权重                     │
│  ┌─────┐      ┌─────┐                  │
│  │  1  │      │ 100 │                  │
│  └─────┘      └─────┘                  │
│                                         │
│  能力声明                               │
│  ☑ Function Calling                    │
│  ☑ Extended Thinking                   │
│  ☑ Vision                              │
│                                         │
│       [测试连接]    [取消]  [保存]      │
└─────────────────────────────────────────┘
```

---

## 六、配置文件

```yaml
# config.yaml

server:
  host: "0.0.0.0"
  port: 8080
  api_key: "fusionapi-xxx"        # 访问 /v1/* 的 key（可选，填 "auto" 自动生成）
  admin_api_key: "fusionapi-admin-xxx"  # 访问 /api/* 的 key（可选，填 "auto" 自动生成）

database:
  path: "./data/fusion.db"

health_check:
  enabled: true
  interval: 60          # 秒
  timeout: 10           # 秒
  failure_threshold: 3  # 连续失败多少次标记不可用

routing:
  strategy: "priority"  # priority | round-robin | weighted | least-latency
  failover:
    enabled: true
    max_retries: 2

logging:
  level: "info"
  retention_days: 7     # 日志保留天数

# 源配置（也可通过 Web UI 管理）
sources:
  - name: "中转站A"
    type: newapi
    base_url: "https://api-a.example.com"
    api_key: "sk-xxx"
    priority: 1
    enabled: true
    capabilities:
      function_calling: true
      extended_thinking: true
      vision: true
```

---

## 七、目录结构

```
fusionapi/
├── cmd/
│   └── fusionapi/
│       └── main.go           # 入口
├── internal/
│   ├── api/
│   │   ├── proxy.go          # 代理 API (/v1/*)
│   │   ├── admin.go          # 管理 API (/api/*)
│   │   └── middleware.go     # 中间件
│   ├── core/
│   │   ├── source.go         # 源管理
│   │   ├── router.go         # 路由决策
│   │   ├── health.go         # 健康检查
│   │   └── translator.go     # 格式转换
│   ├── model/
│   │   ├── source.go         # 源数据模型
│   │   ├── request.go        # 请求/响应模型
│   │   └── log.go            # 日志模型
│   ├── store/
│   │   └── sqlite.go         # SQLite 存储
│   └── config/
│       └── config.go         # 配置加载
├── web/                      # Vue3 前端
│   ├── src/
│   │   ├── views/
│   │   │   ├── Dashboard.vue
│   │   │   ├── Sources.vue
│   │   │   ├── Logs.vue
│   │   │   └── Settings.vue
│   │   ├── components/
│   │   └── api/
│   └── ...
├── config.yaml               # 默认配置
├── Dockerfile
├── docker-compose.yml
└── README.md
```

---

## 八、开发计划

### Phase 1：核心功能 (1周)

- [ ] 项目初始化、目录结构
- [ ] 配置加载
- [ ] Source Manager 基础实现
- [ ] 代理 API `/v1/chat/completions`
- [ ] FC 透传
- [ ] Thinking 透传
- [ ] 流式响应
- [ ] 基础路由（priority）
- [ ] 健康检查

### Phase 2：Web UI + 管理 (1周)

- [ ] 管理 API
- [ ] Vue3 项目搭建
- [ ] 仪表盘页面
- [ ] 源管理页面
- [ ] 设置页面

### Phase 3：完善 (1周)

- [ ] 余额查询
- [ ] 请求日志
- [ ] Failover 逻辑
- [ ] 更多路由策略
- [ ] Docker 打包
- [ ] 文档

---

## 九、资源估算

| 指标 | 预估 |
|------|------|
| 内存占用 | <100MB |
| 二进制大小 | <30MB |
| 并发能力 | 1000+ RPS |
| 磁盘占用 | 取决于日志量 |

---

这份设计你觉得怎么样？有要调整的地方吗？确认后我就开始写代码。
