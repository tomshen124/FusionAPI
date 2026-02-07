package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"sync"

	"github.com/xiaopang/fusionapi/internal/model"
	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
	Routing     RoutingConfig     `yaml:"routing"`
	Logging     LoggingConfig     `yaml:"logging"`
	Sources     []model.Source    `yaml:"sources"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	APIKey      string `yaml:"api_key"`
	AdminAPIKey string `yaml:"admin_api_key"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Enabled          bool `yaml:"enabled"`
	Interval         int  `yaml:"interval"`          // 秒
	Timeout          int  `yaml:"timeout"`           // 秒
	FailureThreshold int  `yaml:"failure_threshold"` // 连续失败阈值
}

// RoutingConfig 路由配置
type RoutingConfig struct {
	Strategy string         `yaml:"strategy"` // priority | round-robin | weighted | least-latency
	Failover FailoverConfig `yaml:"failover"`
}

// FailoverConfig 故障转移配置
type FailoverConfig struct {
	Enabled    bool `yaml:"enabled"`
	MaxRetries int  `yaml:"max_retries"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level         string `yaml:"level"`
	RetentionDays int    `yaml:"retention_days"`
}

var (
	globalConfig *Config
	configMu     sync.RWMutex
)

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// 设置默认值
	setDefaults(cfg)

	// 支持通过 "auto" 自动生成 API Key（首次加载后落盘）
	if maybeGenerateKeys(cfg) {
		if err := Save(path, cfg); err != nil {
			return nil, err
		}
	}

	configMu.Lock()
	globalConfig = cfg
	configMu.Unlock()

	return cfg, nil
}

func maybeGenerateKeys(cfg *Config) bool {
	changed := false

	if strings.EqualFold(strings.TrimSpace(cfg.Server.APIKey), "auto") {
		cfg.Server.APIKey = generateAPIKey("fusionapi-user")
		changed = true
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Server.AdminAPIKey), "auto") {
		cfg.Server.AdminAPIKey = generateAPIKey("fusionapi-admin")
		changed = true
	}

	return changed
}

func generateAPIKey(prefix string) string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return prefix + "-fallback-key"
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// Get 获取全局配置
func Get() *Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return globalConfig
}

// setDefaults 设置默认值
func setDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 18080
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./data/fusion.db"
	}
	if cfg.HealthCheck.Interval == 0 {
		cfg.HealthCheck.Interval = 60
	}
	if cfg.HealthCheck.Timeout == 0 {
		cfg.HealthCheck.Timeout = 10
	}
	if cfg.HealthCheck.FailureThreshold == 0 {
		cfg.HealthCheck.FailureThreshold = 3
	}
	if cfg.Routing.Strategy == "" {
		cfg.Routing.Strategy = "priority"
	}
	if cfg.Routing.Failover.MaxRetries == 0 {
		cfg.Routing.Failover.MaxRetries = 2
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.RetentionDays == 0 {
		cfg.Logging.RetentionDays = 7
	}
}

// Save 保存配置到文件
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
