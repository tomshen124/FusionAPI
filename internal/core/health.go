package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/model"
)

// HealthChecker 健康检查器
type HealthChecker struct {
	manager *SourceManager
	cfg     *config.HealthCheckConfig
	client  *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(manager *SourceManager, cfg *config.HealthCheckConfig) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())
	return &HealthChecker{
		manager: manager,
		cfg:     cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

func (h *HealthChecker) resetContext() {
	h.ctx, h.cancel = context.WithCancel(context.Background())
}

// Start 启动健康检查
func (h *HealthChecker) Start() {
	if !h.cfg.Enabled {
		return
	}
	if h.ctx == nil || h.ctx.Err() != nil {
		h.resetContext()
	}

	h.wg.Add(1)
	go h.run()
}

// Stop 停止健康检查
func (h *HealthChecker) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

// UpdateConfig 动态更新健康检查配置
func (h *HealthChecker) UpdateConfig(cfg *config.HealthCheckConfig) {
	if cfg == nil {
		return
	}

	needRestart := h.cfg.Enabled != cfg.Enabled || h.cfg.Interval != cfg.Interval
	*h.cfg = *cfg
	h.client.Timeout = time.Duration(h.cfg.Timeout) * time.Second

	if needRestart {
		h.Stop()
		h.resetContext()
		if h.cfg.Enabled {
			h.Start()
		}
	}
}

// run 运行健康检查循环
func (h *HealthChecker) run() {
	defer h.wg.Done()

	// 启动时立即检查一次
	h.checkAll()

	ticker := time.NewTicker(time.Duration(h.cfg.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.checkAll()
		}
	}
}

// checkAll 检查所有源
func (h *HealthChecker) checkAll() {
	sources := h.manager.List()
	var wg sync.WaitGroup

	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		wg.Add(1)
		go func(s *model.Source) {
			defer wg.Done()
			h.checkSource(s)
		}(src)
	}

	wg.Wait()
}

// checkSource 检查单个源
func (h *HealthChecker) checkSource(src *model.Source) {
	start := time.Now()
	status := src.GetStatus()
	if status == nil {
		status = &model.SourceStatus{State: model.HealthStateHealthy}
	}

	// Single probe: CPA+AutoDetect uses combined probe+detect, others use plain probe.
	var probeErr error
	if src.Type == model.SourceTypeCPA && src.CPA != nil && src.CPA.AutoDetect {
		probeErr = h.probeAndDetectCPAModels(src, status)
	} else {
		probeErr = h.probeSource(src)
	}
	latency := time.Since(start)

	status.LastCheck = time.Now()
	status.Latency = latency

	if probeErr != nil {
		status.ConsecutiveFail++
		status.ErrorCount++
		status.LastError = probeErr.Error()
		log.Printf("[HealthCheck] %s failed: %v (consecutive: %d)", src.Name, probeErr, status.ConsecutiveFail)

		if status.ConsecutiveFail >= h.cfg.FailureThreshold {
			status.State = model.HealthStateUnhealthy
		}
	} else {
		status.ConsecutiveFail = 0
		status.State = model.HealthStateHealthy
		status.LastError = ""
	}

	src.SetStatus(status)
}

// probeSource 探测源
func (h *HealthChecker) probeSource(src *model.Source) error {
	url := src.BaseURL + "/v1/models"

	req, err := http.NewRequestWithContext(h.ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	// 设置认证头
	h.setAuthHeader(req, src)

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// setAuthHeader 设置认证头
func (h *HealthChecker) setAuthHeader(req *http.Request, src *model.Source) {
	switch src.Type {
	case model.SourceTypeAnthropic:
		req.Header.Set("x-api-key", src.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case model.SourceTypeCPA:
		// CPA 可能不需要 API Key，只在设置了的情况下添加
		if src.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+src.APIKey)
		}
	default:
		req.Header.Set("Authorization", "Bearer "+src.APIKey)
	}
}

// probeAndDetectCPAModels probes a CPA source and detects models in a single /v1/models call.
// This replaces the old pattern of probeSource + detectCPAModels which made two requests.
func (h *HealthChecker) probeAndDetectCPAModels(src *model.Source, status *model.SourceStatus) error {
	url := src.BaseURL + "/v1/models"
	req, err := http.NewRequestWithContext(h.ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	h.setAuthHeader(req, src)

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	// Probe succeeded — now parse model list for auto-detection
	var result struct {
		Data []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Source is reachable but response is unparseable — not a health failure,
		// just skip model detection.
		log.Printf("[CPA] %s: model list decode error: %v", src.Name, err)
		return nil
	}

	modelProviders := make(model.CPAModelProviderMap)
	var detectedModels []string
	detectedProviderSet := make(map[string]bool)
	enabledProviders := src.GetEffectiveCPAProviders()
	enabledProviderSet := make(map[string]bool)
	for _, p := range enabledProviders {
		enabledProviderSet[p] = true
	}

	for _, m := range result.Data {
		if len(enabledProviderSet) > 0 && !enabledProviderSet[m.Provider] {
			continue
		}
		modelProviders[m.ID] = m.Provider
		detectedModels = append(detectedModels, m.ID)
		detectedProviderSet[m.Provider] = true
	}

	status.ModelProviders = modelProviders

	if len(detectedModels) > 0 {
		src.Capabilities.Models = detectedModels
	}

	// Update capability flags based on detected providers (Thinking always off for CPA)
	src.Capabilities.ExtendedThinking = false
	if len(detectedProviderSet) > 0 {
		hasFC := false
		hasVision := false
		for provider := range detectedProviderSet {
			if cap, ok := model.CPAProviderCapabilities[provider]; ok {
				if cap.FC {
					hasFC = true
				}
				if cap.Vision {
					hasVision = true
				}
			}
		}
		src.Capabilities.FunctionCalling = hasFC
		src.Capabilities.Vision = hasVision
	}

	if len(detectedModels) > 0 {
		log.Printf("[CPA] %s: detected %d models from %d providers",
			src.Name, len(detectedModels), countUniqueProviders(modelProviders))
	}

	return nil
}

// countUniqueProviders 统计唯一 provider 数
func countUniqueProviders(mp model.CPAModelProviderMap) int {
	seen := make(map[string]bool)
	for _, p := range mp {
		seen[p] = true
	}
	return len(seen)
}

// CheckBalance 检查余额（仅支持 NewAPI 类型）
func (h *HealthChecker) CheckBalance(src *model.Source) (float64, error) {
	if src.Type != model.SourceTypeNewAPI {
		return 0, fmt.Errorf("balance check not supported for type: %s", src.Type)
	}

	url := src.BaseURL + "/api/user/self"
	req, err := http.NewRequestWithContext(h.ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer "+src.APIKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Quota float64 `json:"quota"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	// 更新状态中的余额
	status := src.GetStatus()
	status.Balance = result.Data.Quota / 500000 // 转换为美元
	src.SetStatus(status)

	return status.Balance, nil
}

// TestConnection 测试源连接
func (h *HealthChecker) TestConnection(src *model.Source) error {
	return h.probeSource(src)
}
