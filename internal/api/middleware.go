package api

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/model"
)

// AuthMiddleware API Key 认证中间件
func AuthMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果未设置 API Key，跳过认证
		if apiKey == "" {
			c.Next()
			return
		}

		// 从 Authorization header 获取 token
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.JSON(401, model.ErrorResponse{
				Error: model.ErrorDetail{
					Message: "Missing Authorization header",
					Type:    "authentication_error",
					Code:    "missing_api_key",
				},
			})
			c.Abort()
			return
		}

		// 解析 Bearer token
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
			// 没有 Bearer 前缀，可能直接是 key
			token = auth
		}

		if token != apiKey {
			c.JSON(401, model.ErrorResponse{
				Error: model.ErrorDetail{
					Message: "Invalid API key",
					Type:    "authentication_error",
					Code:    "invalid_api_key",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// CORSMiddleware CORS 中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// RecoveryMiddleware 恢复中间件
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				c.JSON(500, model.ErrorResponse{
					Error: model.ErrorDetail{
						Message: "Internal server error",
						Type:    "internal_error",
						Code:    "internal_error",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

// LoggerMiddleware 请求日志中间件
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method

		log.Printf("[HTTP] %3d | %12v | %-7s %s",
			status, latency, method, path)
	}
}

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config, proxy *ProxyHandler, admin *AdminHandler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(RecoveryMiddleware())
	r.Use(LoggerMiddleware())
	r.Use(CORSMiddleware())

	// 代理 API（需要认证）
	v1 := r.Group("/v1")
	v1.Use(AuthMiddleware(cfg.Server.APIKey))
	{
		v1.POST("/chat/completions", proxy.ChatCompletions)
		v1.GET("/models", proxy.ListModels)
	}

	// 管理 API（不需要认证，仅限本地访问或内部使用）
	api := r.Group("/api")
	api.Use(AuthMiddleware(cfg.Server.AdminAPIKey))
	{
		// 源管理
		api.GET("/sources", admin.ListSources)
		api.POST("/sources", admin.CreateSource)
		api.GET("/sources/:id", admin.GetSource)
		api.PUT("/sources/:id", admin.UpdateSource)
		api.DELETE("/sources/:id", admin.DeleteSource)
		api.POST("/sources/:id/test", admin.TestSource)
		api.GET("/sources/:id/balance", admin.GetBalance)

		// 状态
		api.GET("/status", admin.GetStatus)
		api.GET("/health", admin.GetHealth)

		// 日志
		api.GET("/logs", admin.GetLogs)
		api.GET("/stats", admin.GetStats)

		// 配置
		api.GET("/config", admin.GetConfig)
		api.PUT("/config", admin.UpdateConfig)
	}

	// 健康检查端点
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// 静态文件服务（Web UI）
	webDir := "./dist/web"
	if _, err := os.Stat(webDir); err == nil {
		r.Static("/assets", filepath.Join(webDir, "assets"))
		r.StaticFile("/favicon.svg", filepath.Join(webDir, "favicon.svg"))

		// SPA fallback
		r.NoRoute(func(c *gin.Context) {
			// API 和 v1 路由返回 404
			if strings.HasPrefix(c.Request.URL.Path, "/api/") ||
				strings.HasPrefix(c.Request.URL.Path, "/v1/") {
				c.JSON(404, gin.H{"error": "not found"})
				return
			}
			// 其他路由返回 index.html
			c.File(filepath.Join(webDir, "index.html"))
		})
	}

	return r
}
