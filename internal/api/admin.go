package api

import (
	"sync"

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
		resp = append(resp, gin.H{
			"id":      src.ID,
			"name":    src.Name,
			"enabled": src.Enabled,
			"state":   status.State,
			"latency": status.Latency.Milliseconds(),
			"balance": status.Balance,
			"error":   status.LastError,
		})
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

	c.JSON(200, gin.H{"message": "Config updated"})
}
