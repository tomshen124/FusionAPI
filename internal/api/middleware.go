package api

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/core"
	"github.com/xiaopang/fusionapi/internal/model"
	"github.com/xiaopang/fusionapi/internal/store"
)

// AuthMiddleware API Key 认证中间件
// Now supports multi-key: checks api_keys table first, then fallback to server.api_key
func AuthMiddleware(apiKey string, st *store.Store, rateLimiter *core.RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Tool detection (always run, even without auth)
		tool := core.DetectTool(c.Request.Header)
		clientIP := c.ClientIP()

		// 如果未设置 API Key，跳过认证 but still set client info
		if apiKey == "" {
			c.Set("client_info", &model.ClientInfo{Tool: tool, IP: clientIP})
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

		// Try to find in api_keys table first
		if st != nil {
			apiKeyObj, err := st.GetAPIKeyByKey(token)
			if err == nil && apiKeyObj != nil {
				// Found a managed key
				if !apiKeyObj.Enabled {
					c.JSON(403, model.ErrorResponse{
						Error: model.ErrorDetail{
							Message: "API key is disabled",
							Type:    "authentication_error",
							Code:    "key_disabled",
						},
					})
					c.Abort()
					return
				}

				// Check allowed tools
				if len(apiKeyObj.AllowedTools) > 0 {
					toolAllowed := false
					for _, t := range apiKeyObj.AllowedTools {
						if t == tool {
							toolAllowed = true
							break
						}
					}
					if !toolAllowed {
						c.JSON(403, model.ErrorResponse{
							Error: model.ErrorDetail{
								Message: "Tool not allowed for this API key",
								Type:    "authentication_error",
								Code:    "tool_not_allowed",
							},
						})
						c.Abort()
						return
					}
				}

				// Check rate limits
				if rateLimiter != nil {
					// Check auto-ban first
					if banned, remaining := rateLimiter.IsAutoBanned(apiKeyObj.ID); banned {
						c.JSON(403, model.ErrorResponse{
							Error: model.ErrorDetail{
								Message: fmt.Sprintf("API key auto-banned due to excessive errors, remaining: %v", remaining.Round(time.Second)),
								Type:    "authentication_error",
								Code:    "key_auto_banned",
							},
						})
						c.Abort()
						return
					}

					allowed, reason := rateLimiter.AllowWithTool(apiKeyObj.ID, apiKeyObj.Limits, tool)
					if !allowed {
						c.JSON(429, model.ErrorResponse{
							Error: model.ErrorDetail{
								Message: reason,
								Type:    "rate_limit_error",
								Code:    "rate_limit_exceeded",
							},
						})
						c.Abort()
						return
					}
				}

				// Update last used (async)
				go st.UpdateAPIKeyLastUsed(apiKeyObj.ID)

				c.Set("client_info", &model.ClientInfo{
					KeyID: apiKeyObj.ID,
					Tool:  tool,
					IP:    clientIP,
				})
				c.Next()
				return
			}
		}

		// Fallback to server.api_key
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

		c.Set("client_info", &model.ClientInfo{Tool: tool, IP: clientIP})
		c.Next()
	}
}

// AdminAuthMiddleware 管理 API 认证中间件（简单单 key 检查）
func AdminAuthMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			c.Next()
			return
		}

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

		token := strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
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
func SetupRouter(cfg *config.Config, proxy *ProxyHandler, admin *AdminHandler, st *store.Store, rateLimiter *core.RateLimiter) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(RecoveryMiddleware())
	r.Use(LoggerMiddleware())
	r.Use(CORSMiddleware())

	// 代理 API（需要认证）
	v1 := r.Group("/v1")
	v1.Use(AuthMiddleware(cfg.Server.APIKey, st, rateLimiter))
	{
		v1.POST("/chat/completions", proxy.ChatCompletions)
		v1.GET("/models", proxy.ListModels)
	}

	// 管理 API（Admin auth - simple single key）
	api := r.Group("/api")
	api.Use(AdminAuthMiddleware(cfg.Server.AdminAPIKey))
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

		// Key management
		api.GET("/keys", admin.ListKeys)
		api.POST("/keys", admin.CreateKey)
		api.GET("/keys/:id", admin.GetKey)
		api.PUT("/keys/:id", admin.UpdateKey)
		api.DELETE("/keys/:id", admin.DeleteKey)
		api.POST("/keys/:id/rotate", admin.RotateKey)
		api.PUT("/keys/:id/block", admin.BlockKey)
		api.PUT("/keys/:id/unblock", admin.UnblockKey)
		api.GET("/keys/:id/usage", admin.GetKeyUsage)

		// Tool stats
		api.GET("/tools/stats", admin.GetToolStats)
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
