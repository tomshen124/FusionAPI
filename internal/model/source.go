package model

import (
	"sync"
	"time"
)

// SourceType 源类型
type SourceType string

const (
	SourceTypeNewAPI    SourceType = "newapi"
	SourceTypeCPA       SourceType = "cpa"
	SourceTypeOpenAI    SourceType = "openai"
	SourceTypeAnthropic SourceType = "anthropic"
	SourceTypeCustom    SourceType = "custom"
)

// HealthState 健康状态
type HealthState string

const (
	HealthStateHealthy   HealthState = "healthy"
	HealthStateUnhealthy HealthState = "unhealthy"
	HealthStateRemoved   HealthState = "removed"
)

// Source API 源配置
type Source struct {
	ID       string     `json:"id" yaml:"id"`
	Name     string     `json:"name" yaml:"name"`
	Type     SourceType `json:"type" yaml:"type"`
	BaseURL  string     `json:"base_url" yaml:"base_url"`
	APIKey   string     `json:"api_key" yaml:"api_key"`
	Priority int        `json:"priority" yaml:"priority"` // 数字越小优先级越高
	Weight   int        `json:"weight" yaml:"weight"`     // 负载均衡权重
	Enabled  bool       `json:"enabled" yaml:"enabled"`

	// 能力声明
	Capabilities Capabilities `json:"capabilities" yaml:"capabilities"`

	// CPA 特有配置
	CPA *CPAConfig `json:"cpa,omitempty" yaml:"cpa,omitempty"`

	// 运行时状态（不持久化到配置）
	Status *SourceStatus `json:"-" yaml:"-"`
	mu     sync.RWMutex  `json:"-" yaml:"-"`
}

// Capabilities 源能力声明
type Capabilities struct {
	FunctionCalling  bool     `json:"function_calling" yaml:"function_calling"`
	ExtendedThinking bool     `json:"extended_thinking" yaml:"extended_thinking"`
	Vision           bool     `json:"vision" yaml:"vision"`
	Models           []string `json:"models" yaml:"models"`
}

// CPAConfig CPA 特有配置
type CPAConfig struct {
	Providers   []string `json:"providers" yaml:"providers"`       // 启用的 provider: gemini, claude, codex, qwen
	AccountMode string   `json:"account_mode" yaml:"account_mode"` // single | multi
	AutoDetect  bool     `json:"auto_detect" yaml:"auto_detect"`   // 自动探测模型和能力
}

// CPA Provider 能力矩阵
var CPAProviderCapabilities = map[string]ProviderCap{
	"gemini": {FC: true, Vision: true},
	"claude": {FC: true, Vision: true},
	"codex":  {FC: true, Vision: true},
	"qwen":   {FC: false, Vision: true},
}

// ProviderCap Provider 能力
type ProviderCap struct {
	FC     bool
	Vision bool
}

// CPAModelProviderMap 模型到 provider 的映射（运行时自动探测填充）
type CPAModelProviderMap map[string]string // model_id -> provider

// SourceStatus 源运行时状态
type SourceStatus struct {
	State           HealthState         `json:"state"`
	Latency         time.Duration       `json:"latency"`
	Balance         float64             `json:"balance"`
	LastCheck       time.Time           `json:"last_check"`
	ErrorCount      int                 `json:"error_count"`
	LastError       string              `json:"last_error"`
	ConsecutiveFail int                 `json:"-"`
	ModelProviders  CPAModelProviderMap `json:"-"` // CPA 探测的模型->provider 映射
}

// SourceStatusResponse 源状态响应（延迟使用毫秒）
type SourceStatusResponse struct {
	State      HealthState `json:"state"`
	Latency    int64       `json:"latency"`
	Balance    float64     `json:"balance"`
	LastCheck  time.Time   `json:"last_check"`
	ErrorCount int         `json:"error_count"`
	LastError  string      `json:"last_error"`
}

// GetStatus 获取状态（线程安全，深拷贝）
func (s *Source) GetStatus() *SourceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Status == nil {
		return &SourceStatus{State: HealthStateHealthy}
	}
	// 返回深拷贝（包括 ModelProviders map）
	status := *s.Status
	if s.Status.ModelProviders != nil {
		status.ModelProviders = make(CPAModelProviderMap, len(s.Status.ModelProviders))
		for k, v := range s.Status.ModelProviders {
			status.ModelProviders[k] = v
		}
	}
	return &status
}

// GetCapabilities 获取能力声明（线程安全，深拷贝）
func (s *Source) GetCapabilities() Capabilities {
	s.mu.RLock()
	defer s.mu.RUnlock()
	caps := s.Capabilities
	if len(s.Capabilities.Models) > 0 {
		caps.Models = make([]string, len(s.Capabilities.Models))
		copy(caps.Models, s.Capabilities.Models)
	}
	return caps
}

// SetCapabilities 设置能力声明（线程安全）
func (s *Source) SetCapabilities(caps Capabilities) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Capabilities = caps
}

// SetStatus 设置状态（线程安全）
func (s *Source) SetStatus(status *SourceStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

// IsHealthy 检查源是否健康
func (s *Source) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Status == nil {
		return true
	}
	return s.Status.State == HealthStateHealthy
}

// SupportsModel 检查源是否支持指定模型（线程安全）
func (s *Source) SupportsModel(model string) bool {
	caps := s.GetCapabilities()
	if len(caps.Models) == 0 {
		return true // 未声明模型列表则认为支持所有
	}
	for _, m := range caps.Models {
		if m == model {
			return true
		}
	}
	return false
}

// SourceResponse 源列表响应（用于API）
type SourceResponse struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Type         SourceType            `json:"type"`
	BaseURL      string                `json:"base_url"`
	Priority     int                   `json:"priority"`
	Weight       int                   `json:"weight"`
	Enabled      bool                  `json:"enabled"`
	Capabilities Capabilities          `json:"capabilities"`
	CPA          *CPAConfig            `json:"cpa,omitempty"`
	Status       *SourceStatusResponse `json:"status,omitempty"`
}

// ToResponse 转换为响应格式（隐藏 API Key）
func (s *Source) ToResponse() SourceResponse {
	status := s.GetStatus()
	caps := s.GetCapabilities()
	var statusResp *SourceStatusResponse
	if status != nil {
		statusResp = &SourceStatusResponse{
			State:      status.State,
			Latency:    status.Latency.Milliseconds(),
			Balance:    status.Balance,
			LastCheck:  status.LastCheck,
			ErrorCount: status.ErrorCount,
			LastError:  status.LastError,
		}
	}

	return SourceResponse{
		ID:           s.ID,
		Name:         s.Name,
		Type:         s.Type,
		BaseURL:      s.BaseURL,
		Priority:     s.Priority,
		Weight:       s.Weight,
		Enabled:      s.Enabled,
		Capabilities: caps,
		CPA:          s.CPA,
		Status:       statusResp,
	}
}

// GetProviderForModel 获取 CPA 源中模型对应的 provider（线程安全）
func (s *Source) GetProviderForModel(modelName string) string {
	status := s.GetStatus()
	if status.ModelProviders != nil {
		if provider, ok := status.ModelProviders[modelName]; ok {
			return provider
		}
	}
	return ""
}

// SupportsFCForModel 检查 CPA 源对特定模型是否支持 FC（线程安全）
func (s *Source) SupportsFCForModel(modelName string) bool {
	if s.Type != SourceTypeCPA {
		caps := s.GetCapabilities()
		return caps.FunctionCalling
	}
	provider := s.GetProviderForModel(modelName)
	if provider != "" && !s.IsCPAProviderEnabled(provider) {
		return false
	}
	if provider == "" {
		// 未探测到 provider 时，基于可用 provider 的能力进行保守判断
		if s.CPA != nil && len(s.CPA.Providers) > 0 {
			for _, p := range s.GetEffectiveCPAProviders() {
				if cap, ok := CPAProviderCapabilities[p]; ok && cap.FC {
					return true
				}
			}
			return false
		}
		caps := s.GetCapabilities()
		return caps.FunctionCalling // fallback 到通用声明
	}
	if cap, ok := CPAProviderCapabilities[provider]; ok {
		return cap.FC
	}
	return false
}

// GetEffectiveCPAProviders 返回当前生效的 provider 列表
func (s *Source) GetEffectiveCPAProviders() []string {
	if s.CPA == nil || len(s.CPA.Providers) == 0 {
		return nil
	}
	providers := append([]string{}, s.CPA.Providers...)
	if s.CPA.AccountMode == "single" && len(providers) > 1 {
		return providers[:1]
	}
	return providers
}

// IsCPAProviderEnabled 判断 provider 是否在当前 CPA 配置中启用
func (s *Source) IsCPAProviderEnabled(provider string) bool {
	if provider == "" {
		return false
	}
	effective := s.GetEffectiveCPAProviders()
	if len(effective) == 0 {
		return true
	}
	for _, p := range effective {
		if p == provider {
			return true
		}
	}
	return false
}
