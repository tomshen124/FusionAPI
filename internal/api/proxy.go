package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/core"
	"github.com/xiaopang/fusionapi/internal/model"
	"github.com/xiaopang/fusionapi/internal/store"
)

// ProxyHandler 代理处理器
type ProxyHandler struct {
	router      *core.Router
	manager     *core.SourceManager
	translator  *core.Translator
	store       *store.Store
	cfg         *config.Config
	client      *http.Client
	rateLimiter *core.RateLimiter
}

// NewProxyHandler 创建代理处理器
func NewProxyHandler(router *core.Router, manager *core.SourceManager, translator *core.Translator, store *store.Store, cfg *config.Config, rateLimiter *core.RateLimiter) *ProxyHandler {
	return &ProxyHandler{
		router:     router,
		manager:    manager,
		translator: translator,
		store:      store,
		cfg:        cfg,
		client: &http.Client{
			Timeout: 5 * time.Minute, // 长超时用于流式响应
		},
		rateLimiter: rateLimiter,
	}
}

// ChatCompletions 聊天补全
func (h *ProxyHandler) ChatCompletions(c *gin.Context) {
	// 解析请求
	var req model.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Invalid request: " + err.Error(),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	// Get client info
	var clientInfo *model.ClientInfo
	if ci, exists := c.Get("client_info"); exists {
		clientInfo = ci.(*model.ClientInfo)
	}

	// Concurrent tracking
	if clientInfo != nil && clientInfo.KeyID != "" && h.rateLimiter != nil {
		h.rateLimiter.AcquireConcurrent(clientInfo.KeyID)
		defer h.rateLimiter.ReleaseConcurrent(clientInfo.KeyID)
	}

	// 记录开始时间
	startTime := time.Now()
	var lastError error
	var triedSources []string
	var failoverFrom string

	// 重试循环
	maxRetries := h.cfg.Routing.Failover.MaxRetries
	if !h.cfg.Routing.Failover.Enabled {
		maxRetries = 0
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 路由选择
		src, err := h.router.RouteRequest(&req, triedSources)
		if err != nil {
			lastError = err
			break
		}

		triedSources = append(triedSources, src.ID)

		// 转换请求
		translatedReq := h.translator.TranslateRequest(&req, src)

		// FC 兼容模式：
		// - 源支持 FC：走原生透传
		// - 源不支持 FC：走兼容层（模拟 tool_call 输出）
		if req.HasTools() && !sourceSupportsFC(src, req.Model) {
			if h.handleFCCompatRequest(c, &req, translatedReq, src, startTime, failoverFrom, clientInfo) {
				return // 成功
			}
		} else {
			// 转发请求
			if req.Stream {
				if h.handleStreamRequest(c, translatedReq, src, startTime, failoverFrom, clientInfo) {
					return // 成功
				}
			} else {
				if h.handleNormalRequest(c, translatedReq, src, startTime, failoverFrom, clientInfo) {
					return // 成功
				}
			}
		}

		// 失败，记录 failover
		failoverFrom = src.ID
		lastError = fmt.Errorf("source %s failed", src.Name)
	}

	// 所有尝试都失败
	h.logRequest(requestIDFromContext(c), &req, nil, nil, startTime, 500, lastError, failoverFrom, clientInfo, false)
	c.JSON(500, model.ErrorResponse{
		Error: model.ErrorDetail{
			Message: "All sources failed: " + lastError.Error(),
			Type:    "upstream_error",
			Code:    "all_sources_failed",
		},
	})
}

// handleNormalRequest 处理非流式请求
func (h *ProxyHandler) handleNormalRequest(c *gin.Context, req *model.ChatCompletionRequest, src *model.Source, startTime time.Time, failoverFrom string, clientInfo *model.ClientInfo) bool {
	// 构建请求
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", src.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false
	}

	h.setHeaders(httpReq, src)

	// 发送请求
	resp, err := h.client.Do(httpReq)
	if err != nil {
		h.updateSourceLatency(src, time.Since(startTime), err)
		return false
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		h.updateSourceLatency(src, time.Since(startTime), fmt.Errorf("status %d", resp.StatusCode))
		return false
	}

	// 解析响应
	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return false
	}

	// 更新延迟
	h.updateSourceLatency(src, time.Since(startTime), nil)

	// 记录日志
	h.logRequest(requestIDFromContext(c), req, &chatResp, src, startTime, resp.StatusCode, nil, failoverFrom, clientInfo, false)

	// 返回响应
	c.JSON(resp.StatusCode, chatResp)
	return true
}

// handleStreamRequest 处理流式请求
func (h *ProxyHandler) handleStreamRequest(c *gin.Context, req *model.ChatCompletionRequest, src *model.Source, startTime time.Time, failoverFrom string, clientInfo *model.ClientInfo) bool {
	// 构建请求
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", src.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false
	}

	h.setHeaders(httpReq, src)

	// 发送请求
	resp, err := h.client.Do(httpReq)
	if err != nil {
		h.updateSourceLatency(src, time.Since(startTime), err)
		return false
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		h.updateSourceLatency(src, time.Since(startTime), fmt.Errorf("status %d", resp.StatusCode))
		return false
	}

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// 流式转发
	reader := bufio.NewReader(resp.Body)
	var totalTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return true // 已开始流式输出，不能回退
		}

		// 跳过空行
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 解析 SSE 数据
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				c.Writer.Flush()
				break
			}

			// 转发数据
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()

			// 尝试解析以统计 token
			var chunk model.StreamChunk
			if json.Unmarshal([]byte(data), &chunk) == nil {
				// 可以在这里统计
			}
		}
	}

	// 更新延迟
	h.updateSourceLatency(src, time.Since(startTime), nil)

	// 记录日志（流式请求无法获取完整 token 统计）
	h.logStreamRequest(requestIDFromContext(c), req, src, startTime, totalTokens, failoverFrom, clientInfo, false)

	return true
}

// setHeaders 设置请求头
func (h *ProxyHandler) setHeaders(req *http.Request, src *model.Source) {
	req.Header.Set("Content-Type", "application/json")

	switch src.Type {
	case model.SourceTypeAnthropic:
		req.Header.Set("x-api-key", src.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case model.SourceTypeCPA:
		if src.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+src.APIKey)
		}
	default:
		req.Header.Set("Authorization", "Bearer "+src.APIKey)
	}
}

// updateSourceLatency 更新源延迟
func (h *ProxyHandler) updateSourceLatency(src *model.Source, latency time.Duration, err error) {
	status := src.GetStatus()
	status.Latency = latency
	status.LastCheck = time.Now()

	if err != nil {
		status.ConsecutiveFail++
		status.ErrorCount++
		status.LastError = err.Error()
		if status.ConsecutiveFail >= h.cfg.HealthCheck.FailureThreshold {
			status.State = model.HealthStateUnhealthy
		}
	} else {
		status.ConsecutiveFail = 0
		status.State = model.HealthStateHealthy
	}

	src.SetStatus(status)
}

// requestIDFromContext gets request id from gin context (if present).
func requestIDFromContext(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(RequestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// logRequest 记录请求日志
func (h *ProxyHandler) logRequest(requestID string, req *model.ChatCompletionRequest, resp *model.ChatCompletionResponse, src *model.Source, startTime time.Time, statusCode int, err error, failoverFrom string, clientInfo *model.ClientInfo, fcCompatUsed bool) {
	log := &model.RequestLog{
		ID:           core.GenerateLogID(),
		RequestID:    requestID,
		Timestamp:    startTime,
		Model:        req.Model,
		HasTools:     req.HasTools(),
		HasThinking:  req.HasThinking(),
		Stream:       req.Stream,
		Success:      err == nil && statusCode == 200,
		StatusCode:   statusCode,
		LatencyMs:    time.Since(startTime).Milliseconds(),
		FailoverFrom: failoverFrom,
		FCCompatUsed: fcCompatUsed,
	}

	if src != nil {
		log.SourceID = src.ID
		log.SourceName = src.Name
	}

	if err != nil {
		log.Error = err.Error()
	}

	if resp != nil && resp.Usage != nil {
		log.PromptTokens = resp.Usage.PromptTokens
		log.CompletionTokens = resp.Usage.CompletionTokens
		log.TotalTokens = resp.Usage.TotalTokens
	}

	// Add client info
	if clientInfo != nil {
		log.ClientIP = clientInfo.IP
		log.ClientTool = clientInfo.Tool
		log.APIKeyID = clientInfo.KeyID
	}

	// Record success/error for auto-ban
	if clientInfo != nil && clientInfo.KeyID != "" && h.rateLimiter != nil {
		if log.Success {
			h.rateLimiter.RecordSuccess(clientInfo.KeyID)
		} else {
			h.rateLimiter.RecordError(clientInfo.KeyID)
		}
	}

	h.store.SaveLog(log)
}

// logStreamRequest 记录流式请求日志
func (h *ProxyHandler) logStreamRequest(requestID string, req *model.ChatCompletionRequest, src *model.Source, startTime time.Time, totalTokens int, failoverFrom string, clientInfo *model.ClientInfo, fcCompatUsed bool) {
	log := &model.RequestLog{
		ID:           core.GenerateLogID(),
		RequestID:    requestID,
		Timestamp:    startTime,
		SourceID:     src.ID,
		SourceName:   src.Name,
		Model:        req.Model,
		HasTools:     req.HasTools(),
		HasThinking:  req.HasThinking(),
		Stream:       true,
		Success:      true,
		StatusCode:   200,
		LatencyMs:    time.Since(startTime).Milliseconds(),
		TotalTokens:  totalTokens,
		FailoverFrom: failoverFrom,
		FCCompatUsed: fcCompatUsed,
	}

	// Add client info
	if clientInfo != nil {
		log.ClientIP = clientInfo.IP
		log.ClientTool = clientInfo.Tool
		log.APIKeyID = clientInfo.KeyID
	}

	// Record success for auto-ban
	if clientInfo != nil && clientInfo.KeyID != "" && h.rateLimiter != nil {
		h.rateLimiter.RecordSuccess(clientInfo.KeyID)
	}

	h.store.SaveLog(log)
}

// ListModels 列出模型
func (h *ProxyHandler) ListModels(c *gin.Context) {
	sources := h.manager.GetHealthy()
	modelSet := make(map[string]bool)
	var models []model.ModelInfo

	for _, src := range sources {
		for _, m := range src.Capabilities.Models {
			if !modelSet[m] {
				modelSet[m] = true
				models = append(models, model.ModelInfo{
					ID:      m,
					Object:  "model",
					Created: time.Now().Unix(),
					OwnedBy: string(src.Type),
				})
			}
		}
	}

	c.JSON(200, model.ModelsResponse{
		Object: "list",
		Data:   models,
	})
}
