package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaopang/fusionapi/internal/api"
	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/core"
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

	// 初始化存储
	db, err := store.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	defer db.Close()
	log.Printf("Database initialized at %s", cfg.Database.Path)

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

	// 使用 http.Server 以支持 Graceful Shutdown
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// 创建一个 context，监听 SIGINT / SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 在 goroutine 中启动 HTTP server
	srvErr := make(chan error, 1)
	go func() {
		log.Printf("FusionAPI starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
		close(srvErr)
	}()

	// 等待信号或服务器错误
	select {
	case err := <-srvErr:
		if err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	case <-ctx.Done():
		log.Println("Shutdown signal received, draining connections...")
	}

	// 给在途请求 15 秒的时间完成
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// 到这里 deferred db.Close() 和 healthChecker.Stop() 会正常执行
	log.Println("Server stopped gracefully")
}
