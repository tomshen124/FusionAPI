package api

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/core"
	"github.com/xiaopang/fusionapi/internal/model"
	"github.com/xiaopang/fusionapi/internal/store"
)

// AdminHandler 管理 API 处理器
type AdminHandler struct {
	manager    *core.SourceManager
	health     *core.HealthChecker
	router     *core.Router
	store      *store.Store
	cfg        *config.Config
	configPath string
	cfgMu      sync.Mutex
}

// NewAdminHandler 创建管理处理器
func NewAdminHandler(manager *core.SourceManager, health *core.HealthChecker, router *core.Router, store *store.Store, cfg *config.Config, configPath string) *AdminHandler {
	return &AdminHandler{
		manager:    manager,
		health:     health,
		router:     router,
		store:      store,
		cfg:        cfg,
		configPath: configPath,
	}
}

// === 源管理 ===

// ListSources 列出所有源
func (h *AdminHandler) ListSources(c *gin.Context) {
	sources := h.manager.List()
	resp := make([]model.SourceResponse, 0, len(sources))
	for _, src := range sources {
		resp = append(resp, src.ToResponse())
	}
	c.JSON(200, gin.H{"data": resp})
}

// CreateSource 创建源
func (h *AdminHandler) CreateSource(c *gin.Context) {
	var src model.Source
	if err := c.ShouldBindJSON(&src); err != nil {
		c.JSON(400, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Invalid request: " + err.Error(),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	// 设置默认值
	if src.Priority == 0 {
		src.Priority = 1
	}
	if src.Weight == 0 {
		src.Weight = 100
	}

	if err := h.manager.Add(&src); err != nil {
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}

	c.JSON(201, gin.H{"data": src.ToResponse()})
}

// GetSource 获取源详情
func (h *AdminHandler) GetSource(c *gin.Context) {
	id := c.Param("id")
	src, ok := h.manager.Get(id)
	if !ok {
		c.JSON(404, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Source not found",
				Type:    "not_found_error",
			},
		})
		return
	}
	c.JSON(200, gin.H{"data": src.ToResponse()})
}

// UpdateSource 更新源
func (h *AdminHandler) UpdateSource(c *gin.Context) {
	id := c.Param("id")

	// 检查是否存在
	existing, ok := h.manager.Get(id)
	if !ok {
		c.JSON(404, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Source not found",
				Type:    "not_found_error",
			},
		})
		return
	}

	var src model.Source
	if err := c.ShouldBindJSON(&src); err != nil {
		c.JSON(400, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Invalid request: " + err.Error(),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	src.ID = id
	// 如果未提供 API Key，保留原有的
	if src.APIKey == "" {
		src.APIKey = existing.APIKey
	}

	if err := h.manager.Update(&src); err != nil {
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}

	c.JSON(200, gin.H{"data": src.ToResponse()})
}

// DeleteSource 删除源
func (h *AdminHandler) DeleteSource(c *gin.Context) {
	id := c.Param("id")
	if err := h.manager.Delete(id); err != nil {
		if err == core.ErrSourceNotFound {
			c.JSON(404, model.ErrorResponse{
				Error: model.ErrorDetail{
					Message: "Source not found",
					Type:    "not_found_error",
				},
			})
			return
		}
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}
	c.JSON(200, gin.H{"message": "Source deleted"})
}

// TestSource 测试源连接
func (h *AdminHandler) TestSource(c *gin.Context) {
	id := c.Param("id")
	src, ok := h.manager.Get(id)
	if !ok {
		c.JSON(404, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Source not found",
				Type:    "not_found_error",
			},
		})
		return
	}

	if err := h.health.TestConnection(src); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{"success": true})
}

// GetBalance 获取源余额
func (h *AdminHandler) GetBalance(c *gin.Context) {
	id := c.Param("id")
	src, ok := h.manager.Get(id)
	if !ok {
		c.JSON(404, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Source not found",
				Type:    "not_found_error",
			},
		})
		return
	}

	balance, err := h.health.CheckBalance(src)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"balance": balance,
	})
}

// === 状态 ===

// GetStatus 获取系统状态
func (h *AdminHandler) GetStatus(c *gin.Context) {
	sources := h.manager.List()

	var healthy, unhealthy, disabled int
	for _, src := range sources {
		if !src.Enabled {
			disabled++
		} else if src.IsHealthy() {
			healthy++
		} else {
			unhealthy++
		}
	}

	c.JSON(200, gin.H{
		"total_sources":     len(sources),
		"healthy_sources":   healthy,
		"unhealthy_sources": unhealthy,
		"disabled_sources":  disabled,
		"routing_strategy":  h.cfg.Routing.Strategy,
		"failover_enabled":  h.cfg.Routing.Failover.Enabled,
	})
}

// GetHealth 获取所有源健康状态
func (h *AdminHandler) GetHealth(c *gin.Context) {
	sources := h.manager.List()
	resp := make([]gin.H, 0, len(sources))

	for _, src := range sources {
		status := src.GetStatus()
		item := gin.H{
			"id":      src.ID,
			"name":    src.Name,
			"enabled": src.Enabled,
			"state":   status.State,
			"latency": status.Latency.Milliseconds(),
			"balance": status.Balance,
			"error":   status.LastError,
		}
		// Include model_providers for CPA sources
		if src.Type == model.SourceTypeCPA && status.ModelProviders != nil {
			item["model_providers"] = status.ModelProviders
		}
		resp = append(resp, item)
	}

	c.JSON(200, gin.H{"data": resp})
}

// === 日志 ===

// GetLogs 获取日志
func (h *AdminHandler) GetLogs(c *gin.Context) {
	var query model.LogQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(400, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Invalid query: " + err.Error(),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	logs, err := h.store.QueryLogs(&query)
	if err != nil {
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}

	c.JSON(200, gin.H{"data": logs})
}

// GetStats 获取统计
func (h *AdminHandler) GetStats(c *gin.Context) {
	days := 7

	dailyStats, err := h.store.GetDailyStats(days)
	if err != nil {
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}

	sourceStats, err := h.store.GetSourceStats(days)
	if err != nil {
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"daily":   dailyStats,
		"sources": sourceStats,
	})
}

// === 配置 ===

// GetConfig 获取配置
func (h *AdminHandler) GetConfig(c *gin.Context) {
	c.JSON(200, gin.H{
		"server": gin.H{
			"host": h.cfg.Server.Host,
			"port": h.cfg.Server.Port,
		},
		"health_check": h.cfg.HealthCheck,
		"routing":      h.cfg.Routing,
		"logging":      h.cfg.Logging,
	})
}

// UpdateConfig 更新配置
func (h *AdminHandler) UpdateConfig(c *gin.Context) {
	var update struct {
		Server *struct {
			Host *string `json:"host"`
			Port *int    `json:"port"`
		} `json:"server"`
		Routing     *config.RoutingConfig     `json:"routing"`
		HealthCheck *config.HealthCheckConfig `json:"health_check"`
		Logging     *config.LoggingConfig     `json:"logging"`
	}

	if err := c.ShouldBindJSON(&update); err != nil {
		c.JSON(400, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Invalid request: " + err.Error(),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	h.cfgMu.Lock()
	defer h.cfgMu.Unlock()

	restartRequired := false

	if update.Server != nil {
		if update.Server.Host != nil {
			host := strings.TrimSpace(*update.Server.Host)
			if host == "" {
				c.JSON(400, model.ErrorResponse{
					Error: model.ErrorDetail{
						Message: "server.host cannot be empty",
						Type:    "invalid_request_error",
					},
				})
				return
			}
			if host != h.cfg.Server.Host {
				h.cfg.Server.Host = host
				restartRequired = true
			}
		}

		if update.Server.Port != nil {
			port := *update.Server.Port
			if port < 1024 || port > 65535 {
				c.JSON(400, model.ErrorResponse{
					Error: model.ErrorDetail{
						Message: "server.port must be between 1024 and 65535",
						Type:    "invalid_request_error",
					},
				})
				return
			}
			if port != h.cfg.Server.Port {
				h.cfg.Server.Port = port
				restartRequired = true
			}
		}
	}

	if update.Routing != nil {
		h.cfg.Routing = *update.Routing
		if h.router != nil {
			h.router.SetStrategy(h.cfg.Routing.Strategy)
		}
	}
	if update.HealthCheck != nil {
		h.cfg.HealthCheck = *update.HealthCheck
		if h.health != nil {
			h.health.UpdateConfig(&h.cfg.HealthCheck)
		}
	}
	if update.Logging != nil {
		h.cfg.Logging = *update.Logging
	}

	if err := config.Save(h.configPath, h.cfg); err != nil {
		c.JSON(500, model.ErrorResponse{
			Error: model.ErrorDetail{
				Message: "Failed to save config: " + err.Error(),
				Type:    "internal_error",
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"message":          "Config updated",
		"restart_required": restartRequired,
	})
}

// === API Keys 管理 ===

// ListKeys 列出所有 API Key
func (h *AdminHandler) ListKeys(c *gin.Context) {
	keys, err := h.store.ListAPIKeys()
	if err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	type KeyWithUsage struct {
		*model.APIKey
		DailyUsage int `json:"daily_usage"`
	}
	result := make([]KeyWithUsage, 0, len(keys))
	for _, k := range keys {
		usage, _ := h.store.GetKeyDailyUsage(k.ID)
		result = append(result, KeyWithUsage{APIKey: k, DailyUsage: usage})
	}
	c.JSON(200, gin.H{"data": result})
}

// CreateKey 创建 API Key
func (h *AdminHandler) CreateKey(c *gin.Context) {
	var input struct {
		Name         string          `json:"name"`
		Limits       model.KeyLimits `json:"limits"`
		AllowedTools []string        `json:"allowed_tools"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, model.ErrorResponse{Error: model.ErrorDetail{Message: "Invalid request: " + err.Error(), Type: "invalid_request_error"}})
		return
	}

	key := &model.APIKey{
		ID:           core.GenerateKeyID(),
		Key:          core.GenerateAPIKey(),
		Name:         input.Name,
		Enabled:      true,
		Limits:       input.Limits,
		AllowedTools: input.AllowedTools,
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
	}

	if err := h.store.SaveAPIKey(key); err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(201, gin.H{"data": key})
}

// GetKey 获取 API Key 详情
func (h *AdminHandler) GetKey(c *gin.Context) {
	id := c.Param("id")
	key, err := h.store.GetAPIKey(id)
	if err != nil {
		c.JSON(404, model.ErrorResponse{Error: model.ErrorDetail{Message: "Key not found", Type: "not_found_error"}})
		return
	}
	usage, _ := h.store.GetKeyDailyUsage(id)
	c.JSON(200, gin.H{"data": key, "daily_usage": usage})
}

// UpdateKey 更新 API Key
func (h *AdminHandler) UpdateKey(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.store.GetAPIKey(id)
	if err != nil {
		c.JSON(404, model.ErrorResponse{Error: model.ErrorDetail{Message: "Key not found", Type: "not_found_error"}})
		return
	}

	var input struct {
		Name         *string          `json:"name"`
		Limits       *model.KeyLimits `json:"limits"`
		AllowedTools *[]string        `json:"allowed_tools"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, model.ErrorResponse{Error: model.ErrorDetail{Message: "Invalid request: " + err.Error(), Type: "invalid_request_error"}})
		return
	}

	if input.Name != nil {
		existing.Name = *input.Name
	}
	if input.Limits != nil {
		existing.Limits = *input.Limits
	}
	if input.AllowedTools != nil {
		existing.AllowedTools = *input.AllowedTools
	}

	if err := h.store.SaveAPIKey(existing); err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"data": existing})
}

// DeleteKey 删除 API Key
func (h *AdminHandler) DeleteKey(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeleteAPIKey(id); err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"message": "Key deleted"})
}

// RotateKey 轮换 API Key
func (h *AdminHandler) RotateKey(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.store.GetAPIKey(id)
	if err != nil {
		c.JSON(404, model.ErrorResponse{Error: model.ErrorDetail{Message: "Key not found", Type: "not_found_error"}})
		return
	}
	existing.Key = core.GenerateAPIKey()
	if err := h.store.SaveAPIKey(existing); err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"data": existing})
}

// BlockKey 禁用 API Key
func (h *AdminHandler) BlockKey(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.store.GetAPIKey(id)
	if err != nil {
		c.JSON(404, model.ErrorResponse{Error: model.ErrorDetail{Message: "Key not found", Type: "not_found_error"}})
		return
	}
	existing.Enabled = false
	if err := h.store.SaveAPIKey(existing); err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"data": existing})
}

// UnblockKey 启用 API Key
func (h *AdminHandler) UnblockKey(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.store.GetAPIKey(id)
	if err != nil {
		c.JSON(404, model.ErrorResponse{Error: model.ErrorDetail{Message: "Key not found", Type: "not_found_error"}})
		return
	}
	existing.Enabled = true
	if err := h.store.SaveAPIKey(existing); err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"data": existing})
}

// GetToolStats 获取工具使用统计
func (h *AdminHandler) GetToolStats(c *gin.Context) {
	stats, err := h.store.GetToolStats(7)
	if err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"data": stats})
}

// GetKeyUsage 获取 Key 使用趋势
func (h *AdminHandler) GetKeyUsage(c *gin.Context) {
	id := c.Param("id")
	days := 7
	if d := c.Query("days"); d != "" {
		var n int
		if _, err := fmt.Sscanf(d, "%d", &n); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	usages, err := h.store.GetKeyUsageTrend(id, days)
	if err != nil {
		c.JSON(500, model.ErrorResponse{Error: model.ErrorDetail{Message: err.Error(), Type: "internal_error"}})
		return
	}
	c.JSON(200, gin.H{"data": usages})
}
