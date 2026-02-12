package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaopang/fusionapi/internal/api"
	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/core"
	"github.com/xiaopang/fusionapi/internal/logger"
	"github.com/xiaopang/fusionapi/internal/store"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Config loaded from %s", *configPath)

	// Apply logging level
	logger.SetLevel(logger.ParseLevel(cfg.Logging.Level))
	logger.Info("logging configured", "level", cfg.Logging.Level)

	// 初始化存储
	db, err := store.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	defer db.Close()
	log.Printf("Database initialized at %s", cfg.Database.Path)

	// Initialize log retention cleanup
	if cfg.Logging.RetentionDays > 0 {
		if deleted, err := db.CleanOldLogs(cfg.Logging.RetentionDays); err != nil {
			logger.Warn("log retention cleanup failed", "err", err, "retention_days", cfg.Logging.RetentionDays)
		} else if deleted > 0 {
			logger.Info("log retention cleanup", "deleted", deleted, "retention_days", cfg.Logging.RetentionDays)
		}

		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				if deleted, err := db.CleanOldLogs(cfg.Logging.RetentionDays); err != nil {
					logger.Warn("log retention cleanup failed", "err", err, "retention_days", cfg.Logging.RetentionDays)
				} else if deleted > 0 {
					logger.Info("log retention cleanup", "deleted", deleted, "retention_days", cfg.Logging.RetentionDays)
				}
			}
		}()
	}

	// 初始化源管理器
	manager := core.NewSourceManager(db)

	// 从数据库加载源
	if err := manager.Load(); err != nil {
		log.Printf("Warning: failed to load sources from db: %v", err)
	}

	// 从配置文件加载源（会合并到数据库）
	if len(cfg.Sources) > 0 {
		if err := manager.LoadFromConfig(cfg.Sources); err != nil {
			log.Printf("Warning: failed to load sources from config: %v", err)
		}
		log.Printf("Loaded %d sources from config", len(cfg.Sources))
	}

	// 初始化路由器
	router := core.NewRouter(manager, cfg.Routing.Strategy)
	log.Printf("Router initialized with strategy: %s", cfg.Routing.Strategy)

	// 初始化健康检查器
	healthChecker := core.NewHealthChecker(manager, &cfg.HealthCheck)
	healthChecker.Start()
	defer healthChecker.Stop()
	if cfg.HealthCheck.Enabled {
		log.Printf("Health checker started (interval: %ds)", cfg.HealthCheck.Interval)
	}

	// 初始化转换器
	translator := core.NewTranslator()

	// 初始化频率限制器
	rateLimiter := core.NewRateLimiter()

	// 初始化 API 处理器
	proxyHandler := api.NewProxyHandler(router, manager, translator, db, cfg, rateLimiter)
	adminHandler := api.NewAdminHandler(manager, healthChecker, router, db, cfg, *configPath)

	// 设置路由
	r := api.SetupRouter(cfg, proxyHandler, adminHandler, db, rateLimiter)

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("FusionAPI starting on %s", addr)

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
