# FusionAPI 竞品/同类项目调研与优化建议（Competitive Analysis）

> 面向：FusionAPI（Go + Vue3）——OpenAI-compatible 的多上游聚合网关
> 
> 目标：对标/借鉴 LiteLLM、one-api/new-api、商业 AI Gateway（Portkey/Kong/Cloudflare），给出 **“FusionAPI 缺什么、可从哪里借鉴什么”** 的可落地优化清单（按优先级 + 实现难度排序）。

## 0. FusionAPI 当前能力速览（基于 README + 代码）

FusionAPI 已具备一个“轻量聚合网关”的关键骨架：

- **OpenAI 兼容入口**：`/v1/chat/completions`、`/v1/models`
- **多上游 sources**：NewAPI、CPA、OpenAI、Anthropic、Custom(OpenAI-compatible)
- **健康检查 + 故障切换**：探测 `/v1/models`；失败阈值后标记 unhealthy
- **路由策略**：`priority` / `round-robin` / `weighted` / `least-latency` / `least-cost(按余额)`
- **能力路由**：Function Calling / Extended Thinking / Vision
- **FC 降级**：没有 FC-capable source 时可去掉 tool 字段降级到非 FC 源
- **多 key 管理（面向调用方）**：每 key RPM、日配额、并发；工具识别/白名单；block/unblock/rotate；自动封禁（连续错误阈值）
- **轻部署**：单二进制 + SQLite；Web UI 动态改配置并持久化

> 结论：FusionAPI 已经是一个“能用、易部署”的聚合网关，但与最流行/最成熟方案相比，**缺少“企业/平台化能力”（多租户、成本/预算、可观测性、策略化路由、缓存/guardrails、更多 OpenAI endpoints 支持等）**。

---

## 1) LiteLLM（OSS 最流行的 LLM Proxy/Gateway）— 可借鉴点

核心定位：**统一调用 100+ LLM（OpenAI 格式或原生格式）+ Proxy 网关**，并提供平台化能力（虚拟 key、多租户、成本、可观测性、插件、缓存、可靠性路由）。

### 1.1 FusionAPI 可以直接借鉴的“强价值功能”

1. **虚拟 Key / Team / User 多层级的配额 & 预算（Spend Tracking）**
   - LiteLLM 的 Virtual Keys 支持：按 key/user/team 追踪消耗、设置预算上限与周期（budget_duration），并可通过 API/控制台查看。
   - 参考：Virtual Keys & Spend Tracking（LiteLLM docs）
     - https://docs.litellm.ai/docs/proxy/virtual_keys
     - https://docs.litellm.ai/docs/proxy/team_budgets
     - https://docs.litellm.ai/docs/proxy/metrics

   **FusionAPI 缺什么？**
   - 目前是“请求次数配额（RPM/日/并发）”，但没有：
     - token 维度（TPM/日 tokens）
     - 金额维度（按模型价格表估算成本、预算、账单）
     - team/user 层级（多租户）

2. **Rate-limit aware / cooldown / retry 的“可靠性路由”**
   - LiteLLM Router 支持：基于 rpm/tpm、least-busy、latency、cost 等做策略路由，并提供**cooldown（熔断/冷却）**、超时、重试（含指数退避）、fallback。
   - 参考：Routing/Router 文档
     - https://docs.litellm.ai/docs/routing

   **FusionAPI 现状与缺口**
   - 已有策略：priority/rr/weighted/least-latency/least-cost
   - 但缺少：
     - “**按上游返回的 rate limit headers**（remaining requests/tokens）动态避开”
     - “**cooldown / half-open**”等更标准的熔断策略（目前健康检查是周期性探测；对请求级失败处理可更细）
     - “**按错误类型触发 fallback**”（例如 429/5xx/timeout 才重试，4xx 不重试）

3. **Prometheus 指标与可观测性回调（Observability callbacks）**
   - LiteLLM 提供 `/metrics`（Prometheus），指标覆盖请求量、失败、延迟、TTFT、fallback 次数、预算剩余等，并可把自定义 metadata 作为 label（控制基数）。
   - 参考：Prometheus
     - https://docs.litellm.ai/docs/proxy/prometheus

   **FusionAPI 缺什么？**
   - 当前有日志/统计接口，但缺少标准指标出口（Prometheus/OpenMetrics）和 trace（OTel）。

4. **Proxy Hook / Plugin 机制（对请求/响应做策略与改写）**
   - LiteLLM 的 call hooks 支持：pre-call 修改请求、并行 moderation hook、post-call 修改响应、统一错误转换、注入响应 header 等。
   - 参考：Call Hooks
     - https://docs.litellm.ai/docs/proxy/call_hooks

   **FusionAPI 缺什么？**
   - 目前能力主要写死在 translator/router/core 中；缺少可插拔扩展点（例如：自定义鉴权、黑白名单、内容审计、脱敏、缓存键策略、特定客户的字段兼容）。

5. **缓存（含语义缓存）**
   - LiteLLM 支持多种缓存后端（memory/disk/redis/s3/gcs/qdrant semantic cache 等），并支持 per-request 控制 TTL/namespace/no-store 等。
   - 参考：Caching
     - https://docs.litellm.ai/docs/proxy/caching

   **FusionAPI 缺什么？**
   - 目前无任何响应缓存；在高频重复请求（FAQ/客服/模板化提示词）场景，会显著增加成本与延迟。

6. **Model aliases / model group（模型映射与分层能力）**
   - LiteLLM key 可以附带 aliases，把请求的模型名映射到内部模型组（实现灰度、升降级）。
   - 参考：Virtual Keys 中的 Model Aliases（同上 virtual_keys 页面）

   **FusionAPI 可借鉴**
   - 将“模型名映射/灰度/AB”做成一等公民（而不是仅仅 source capabilities.models）。

### 1.2 LiteLLM 对 FusionAPI 的产品启发

- LiteLLM Proxy 之所以成为事实标准：不仅是“能转发”，更是“**可运营**”：多租户、预算、观测、插件、缓存、可靠性路由。
- FusionAPI 如果要做成团队/公司内的统一网关，下一阶段应重点补齐：
  1) 可观测性标准化（metrics + trace）
  2) 成本/预算
  3) 策略化路由（可配置）
  4) 可插拔 hook

---

## 2) one-api / new-api（Go 生态最常见的 OpenAI 接口管理面板）— 路由与 key 管理对比

核心定位：**“key 管理 + 渠道(channel)/供应商管理 + 计费/配额 + 分发”**，更偏“运营面板”，而不是纯网关。

### 2.1 路由策略（渠道维度）

来自其 README / new-api 文档可见：

- 支持多个“渠道（channel）”配置，并可做 **负载均衡**（weighted random 等）
- 支持 **失败自动重试**
- 支持 **模型映射（model mapping / redirect）**：把用户请求的 model 重定向到另一个（提示会重构请求体，可能丢字段）
- 支持 **分组与倍率**：用户分组、渠道分组，不同组走不同倍率/策略
- 支持“令牌后缀指定渠道 ID”（管理员 token 可强制某次请求走某 channel）

参考：
- one-api README（功能列表） https://github.com/songquanpeng/one-api
- new-api README（智能路由、weighted random、rate limiting 等） https://github.com/QuantumNous/new-api
- new-api Features Introduction（model rate limiting、cache billing 等） https://docs.newapi.pro/en/docs/guide/wiki/basic-concepts/features-introduction

**FusionAPI 现状**
- routing strategy 是“source 级”策略（优先级/权重/延迟/余额）。

**FusionAPI 缺口/可借鉴**
- “**模型映射/重定向**”（适用于：统一对外 model 命名，内部随时换供应商；以及不同客户走不同模型）
- “**分组/租户策略**”：不同 key（或 user/team）绑定到不同 sources/model policy
- “**请求级强制路由**”：提供类似 `X-Route-Source` / `X-Route-Group`（管理员）或 key-policy 内置固定路由
- “**多机部署一致性**”：one-api/new-api 强调 MySQL + Redis 做多实例一致性与缓存

### 2.2 Key 管理（令牌/权限/配额）

one-api/new-api 的 token 管理更平台化：
- token 过期时间
- 配额/额度
- 允许 IP 范围
- 允许访问的模型列表
- 用户系统（注册/登录/邀请/充值等）与权限管理

**FusionAPI 现状**
- 多 key + RPM/日/并发 + tool whitelist，偏“网关侧访问控制”。

**FusionAPI 缺口/可借鉴（按价值排序）**
1. **模型 allowlist/denylist（每 key）**：对企业内部非常常见（避免使用昂贵模型/不合规模型）
2. **IP allowlist（每 key）**：边界防护 + 降低 key 泄露风险
3. **token 过期/时间窗预算**：临时 key、CI key、短期外包访问
4. （若走平台化路线）用户体系、充值/计费、分组倍率

> 取舍建议：FusionAPI 不一定要做 one-api/new-api 那么重的“运营系统”，但可以抽取其最实用的安全/权限能力（模型/IP/过期）。

---

## 3) 商业/企业 AI Gateway（Portkey / Kong / Cloudflare）— 核心功能对比

商业网关共同点：
- 强可观测性（日志 + 指标 + Trace）
- 可靠性（重试/超时/fallback/动态路由）
- 安全治理（密钥托管、RBAC、审计、guardrails/DLP）
- 成本优化（缓存、预算、分析、成本估算）

### 3.1 Cloudflare AI Gateway（典型“平台化策略路由”）

Cloudflare AI Gateway 明确提供：
- Analytics / Logging
- Caching
- Rate limiting（固定/滑动窗口）
- Request retry & model fallback
- **Dynamic Routing**：用可视化/JSON 配置路由流，支持条件、百分比分流、rate/budget limit 节点、版本化与回滚
- **BYOK keys**：把上游 provider keys 存在 Cloudflare Secrets Store，避免应用侧携带
- **OTel traces**：导出 OTLP/JSON，并遵循 GenAI semantic conventions（记录模型、tokens、cost、prompt/response、metadata 等）
- Guardrails（内容安全）

参考：
- Overview https://developers.cloudflare.com/ai-gateway/
- Getting started https://developers.cloudflare.com/ai-gateway/get-started/
- Dynamic routing https://developers.cloudflare.com/ai-gateway/features/dynamic-routing/
- Caching https://developers.cloudflare.com/ai-gateway/features/caching/
- Rate limiting https://developers.cloudflare.com/ai-gateway/features/rate-limiting/
- BYOK https://developers.cloudflare.com/ai-gateway/configuration/bring-your-own-keys/
- OTel integration https://developers.cloudflare.com/ai-gateway/observability/otel-integration/
- Guardrails https://developers.cloudflare.com/ai-gateway/features/guardrails/

**FusionAPI 可借鉴点**
- “路由即配置（routing as code）”：把路由从固定策略升级为 **可组合的策略图/规则引擎**（条件、分流、预算、回退）。
- OTel：跟现有可观测体系（Jaeger/Tempo/Datadog/Honeycomb/Langfuse 等）融合。
- BYOK：在网关侧集中托管上游 key（支持 alias/轮换），应用只拿网关 key。

### 3.2 Portkey AI Gateway（开源 + 强调 guardrails/可靠性/多模态）

Portkey 开源网关的 README 中强调：
- automatic retries / fallbacks
- load balancing / conditional routing
- guardrails（输入输出检查）
- caching（含 semantic caching）
- secure key management（virtual keys）
- 多模态（vision/audio/image），并提供 Console
- MCP Gateway：集中管理 MCP server 的鉴权、访问控制、可观测性

参考：
- https://github.com/Portkey-AI/gateway

**FusionAPI 可借鉴点**
- guardrails（可插拔规则）+ retry/fallback 与 guardrail 联动（被拒绝就重试/换模型）
- 对 MCP 的适配：FusionAPI 已有“tool detection”，可以进一步扩展到“**MCP/工具调用治理**”（审计、审批、策略）。

### 3.3 Kong AI Gateway（企业级 API Gateway 体系里的 AI 插件化）

Kong 的 AI Proxy plugin 特点：
- 接受 OpenAI 标准格式并转换到多 provider
- 覆盖更多 route types：chat、embeddings、audio、images、batches/files/assistants/responses 等
- 支持 native format passthrough（不转换也能做观测/计费）
- 依托 Kong 现有生态：认证、限流、日志、追踪、WAF、插件等

参考：
- Kong AI Proxy plugin https://developer.konghq.com/plugins/ai-proxy/

**FusionAPI 可借鉴点**
- “路由类型/端点覆盖”是网关能力的关键：不只 chat completions
- “插件化”是可扩展性的核心：认证、限流、日志、审计都可复用通用网关模型

---

## 4) 行业最佳实践（限流、熔断、可观测性、fallback）

结合 LiteLLM / Cloudflare / Kong 的共性，给出“网关实现标准做法”清单：

### 4.1 限流（Rate limiting）

**最佳实践**
- 维度：
  - per API key（你已有）
  - per user/team/project（多租户）
  - per model（避免某模型被打爆）
  - per upstream deployment/provider（保护上游）
- 指标：
  - RPM（requests/min）
  - TPM（tokens/min）——更贴近真实成本与上游限制
  - 并发（in-flight）——避免队列堆积
- 算法：固定窗口 vs 滑动窗口；令牌桶（token bucket）
- **分布式一致性**：多实例部署时用 Redis/KeyDB 维护计数与并发
- 429 返回时建议包含 `Retry-After`，并记录到 metrics

**对 FusionAPI 的落地建议**
- P0：在现有 RPM/日/并发基础上，补 TPM（估算 tokens：
  - 简化版：根据 OpenAI 返回 usage 记账；
  - 进阶：预估 prompt tokens（tiktoken/claude tokenizer 需要跨语言实现，可先“事后记账 + 超限封禁”）。
- P1：把 rate limiter 从内存 map 升级为可选 Redis（多实例）。

### 4.2 熔断（Circuit breaker）

**最佳实践**
- 熔断对象：每个 upstream deployment/source
- 三态：Closed（正常）→ Open（熔断）→ Half-open（探测恢复）
- 触发信号：
  - 连续失败数
  - 错误率（rolling window）
  - 超时/连接错误
  - 429/5xx（可配置是否算失败）
- Open 状态设置 cooldown（例如 30s/60s/5min），到期进入 half-open 允许少量探测流量

**对 FusionAPI 的落地建议**
- 你已有 health check，但它是“定时探测”，缺少“请求内快速熔断”。
- P0：在转发失败时对 source 进行短时 cooldown（类似 LiteLLM cooldown）——避免故障源被持续选中。

### 4.3 可观测性（Observability）

**最佳实践**
- 日志：结构化（JSON），含 request_id、key_id、model、source_id、latency、status_code、tokens、cost、error_class
- Metrics：Prometheus/OpenMetrics
  - request_total / error_total（按 model/source/status）
  - latency histogram + TTFT（streaming）
  - fallback/retry 次数
  - remaining_budget（key/team）
- Trace：OpenTelemetry
  - span：gateway → upstream
  - attributes：gen_ai.request.model、provider、tokens、cost、prompt/response（敏感数据可配置不采集）

**对 FusionAPI 的落地建议**
- P0：增加 `/metrics`（Prometheus）
- P1：OTel trace export（与 Cloudflare 做法一致）

### 4.4 Fallback/重试（Reliability）

**标准做法**
- 明确“可重试错误集合”：timeout、connect reset、5xx、429（可选）
- 明确“不重试错误”：多数 4xx（参数错误、鉴权失败）
- 指数退避 + jitter
- 对 streaming：
  - 若已输出部分 token，再 fallback 会造成重复输出；通常策略是：
    - 仅在“未输出任何 token/TTFT 前”才允许自动 fallback
    - 或者做“hedged request”（并发发两个，上游返回先到者；成本更高但可做高优先级请求）

**对 FusionAPI 的落地建议**
- 现有 failover：有“max_retries + 排除已试 source”，但建议升级为：
  - 按错误类型决定是否 retry/fallback
  - 对 streaming 采用“TTFT 前 fallback”策略

---

## 5) FusionAPI 缺什么？（按优先级 + 难度排序的改进路线图）

> 评分说明：
> - 优先级：P0（强烈建议下一版本做）/ P1（重要）/ P2（可选增强）
> - 难度：S（小）/ M（中）/ L（大）

### P0（高价值、相对可控）

1. **Prometheus `/metrics` + 关键指标体系**（难度：M）
   - 借鉴：LiteLLM Prometheus、Kong logging、Cloudflare analytics
   - 指标建议：
     - gateway_requests_total{model,source,api_key,tool,status}
     - gateway_request_latency_seconds_bucket（直方图）
     - gateway_upstream_errors_total{source,error_class}
     - gateway_fallback_total{from_source,to_source,reason}
   - 好处：立刻提升可运维性；后续做 SLO/熔断/自动路由的基础。

2. **请求级熔断/冷却（cooldown）+ 更细粒度 failover**（难度：M）
   - 借鉴：LiteLLM cooldown、标准 circuit breaker
   - 做法：在每次 upstream 失败时更新 source 的 breaker 状态；路由时跳过 Open 状态源；half-open 用少量探测。

3. **错误分类与策略化重试（exponential backoff + jitter）**（难度：M）
   - 借鉴：LiteLLM reliability logic、Cloudflare retry/fallback
   - 关键：区分 429/5xx/timeout vs 4xx；streaming 的 TTFT 前后差异。

4. **每 key 的模型 allowlist/denylist + 过期时间**（难度：S~M）
   - 借鉴：one-api/new-api 的 token 模型限制、过期
   - 价值：立刻增强安全性与成本控制（不给某些 key 调昂贵模型）。

### P1（平台化能力增强）

5. **成本/消耗（tokens/cost）追踪 + 预算（budget）**（难度：L）
   - 借鉴：LiteLLM spend tracking、Cloudflare costs
   - 最小可用：
     - 先“事后记账”：用 upstream response 的 `usage` 记录 prompt/completion/total tokens
     - 基于内置/可更新的 `model_prices.json` 估算 cost
     - key 级预算上限（按日/按月）

6. **缓存（Response cache + 可选语义缓存）**（难度：M~L）
   - 借鉴：Cloudflare caching（header 控制）、LiteLLM caching（redis/semantic）
   - 建议分阶段：
     - 阶段 1：精确缓存（相同 request body 哈希 + TTL）
     - 阶段 2：语义缓存（需要向量库/embedding；复杂度显著上升）

7. **可插拔 Hook/Plugin 机制**（难度：L）
   - 借鉴：LiteLLM call hooks、Kong plugin 生态
   - 用途：自定义鉴权、内容审计/脱敏、缓存键策略、强制 user 字段、返回头注入、错误转换。

8. **策略化路由（条件/百分比/预算/限流节点）**（难度：L）
   - 借鉴：Cloudflare Dynamic Routing
   - 目标：把 routing 从“单一策略字符串”升级为“规则/flow”，支持：
     - paid/free 用户走不同模型
     - AB test（percentage split）
     - budget/rate limit exceeded 自动降级

### P2（生态扩展与更完整的 OpenAI 兼容面）

9. **更多 OpenAI endpoints 覆盖**（难度：M~L）
   - 借鉴：Kong AI Gateway、LiteLLM supported endpoints
   - 优先顺序建议：
     1) `/v1/embeddings`
     2) `/v1/images/generations`
     3) `/v1/audio/transcriptions`、`/v1/audio/speech`
     4) `/v1/responses`（新 OpenAI API 形态）

10. **BYOK 上游 Key 托管 + alias/轮换**（难度：M）
   - 借鉴：Cloudflare BYOK
   - 价值：应用侧不再携带 provider key，降低泄露风险；可在网关统一轮换。

11. **MCP / 工具调用治理**（难度：L）
   - 借鉴：Portkey MCP Gateway
   - 方向：对 tool_calls / MCP server 做审计、权限、速率、审批、可观测。

---

## 6) 建议的“最小可用增强”组合（推荐先做的 3 件事）

如果只选 3 件最划算的改进：

1. **Prometheus metrics（P0）**：没有指标就无法做 SLO、熔断与自动路由。
2. **请求级 cooldown/circuit breaker（P0）**：显著提升稳定性，减少“坏源拖累”。
3. **模型访问控制（per-key allowlist + 过期）（P0）**：立刻提升安全与成本治理。

---

## 7) 参考链接（本报告引用/阅读的主要资料）

- FusionAPI README（本地）
- LiteLLM
  - GitHub: https://github.com/BerriAI/litellm
  - Virtual Keys: https://docs.litellm.ai/docs/proxy/virtual_keys
  - Team budgets: https://docs.litellm.ai/docs/proxy/team_budgets
  - Routing: https://docs.litellm.ai/docs/routing
  - Call hooks: https://docs.litellm.ai/docs/proxy/call_hooks
  - Caching: https://docs.litellm.ai/docs/proxy/caching
  - Prometheus: https://docs.litellm.ai/docs/proxy/prometheus
- one-api / new-api
  - one-api: https://github.com/songquanpeng/one-api
  - new-api: https://github.com/QuantumNous/new-api
  - new-api docs: https://docs.newapi.pro/
- Cloudflare AI Gateway
  - Overview: https://developers.cloudflare.com/ai-gateway/
  - Dynamic routing: https://developers.cloudflare.com/ai-gateway/features/dynamic-routing/
  - Caching: https://developers.cloudflare.com/ai-gateway/features/caching/
  - OTel: https://developers.cloudflare.com/ai-gateway/observability/otel-integration/
  - BYOK: https://developers.cloudflare.com/ai-gateway/configuration/bring-your-own-keys/
  - Guardrails: https://developers.cloudflare.com/ai-gateway/features/guardrails/
- Portkey Gateway
  - GitHub: https://github.com/Portkey-AI/gateway
- Kong AI Gateway (AI Proxy plugin)
  - https://developer.konghq.com/plugins/ai-proxy/
