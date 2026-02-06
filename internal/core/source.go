package core

import (
	"sync"

	"github.com/xiaopang/fusionapi/internal/model"
	"github.com/xiaopang/fusionapi/internal/store"
)

// SourceManager 源管理器
type SourceManager struct {
	sources map[string]*model.Source
	store   *store.Store
	mu      sync.RWMutex
}

// NewSourceManager 创建源管理器
func NewSourceManager(s *store.Store) *SourceManager {
	return &SourceManager{
		sources: make(map[string]*model.Source),
		store:   s,
	}
}

// Load 从存储加载所有源
func (m *SourceManager) Load() error {
	sources, err := m.store.ListSources()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sources = make(map[string]*model.Source)
	for _, src := range sources {
		// 初始化运行时状态
		src.Status = &model.SourceStatus{
			State: model.HealthStateHealthy,
		}
		m.sources[src.ID] = src
	}
	return nil
}

// LoadFromConfig 从配置加载源
func (m *SourceManager) LoadFromConfig(sources []model.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range sources {
		src := &sources[i]
		if src.ID == "" {
			src.ID = generateID()
		}
		src.Status = &model.SourceStatus{
			State: model.HealthStateHealthy,
		}
		m.sources[src.ID] = src

		// 同步到存储
		if m.store != nil {
			m.store.SaveSource(src)
		}
	}
	return nil
}

// Add 添加源
func (m *SourceManager) Add(src *model.Source) error {
	if src.ID == "" {
		src.ID = generateID()
	}

	// 初始化状态
	src.Status = &model.SourceStatus{
		State: model.HealthStateHealthy,
	}

	// 保存到存储
	if err := m.store.SaveSource(src); err != nil {
		return err
	}

	m.mu.Lock()
	m.sources[src.ID] = src
	m.mu.Unlock()

	return nil
}

// Update 更新源
func (m *SourceManager) Update(src *model.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.sources[src.ID]
	if !ok {
		return ErrSourceNotFound
	}

	// 保留运行时状态
	src.Status = existing.Status

	// 保存到存储
	if err := m.store.SaveSource(src); err != nil {
		return err
	}

	m.sources[src.ID] = src
	return nil
}

// Delete 删除源
func (m *SourceManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sources[id]; !ok {
		return ErrSourceNotFound
	}

	if err := m.store.DeleteSource(id); err != nil {
		return err
	}

	delete(m.sources, id)
	return nil
}

// Get 获取源
func (m *SourceManager) Get(id string) (*model.Source, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src, ok := m.sources[id]
	return src, ok
}

// List 列出所有源
func (m *SourceManager) List() []*model.Source {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sources := make([]*model.Source, 0, len(m.sources))
	for _, src := range m.sources {
		sources = append(sources, src)
	}
	return sources
}

// GetHealthy 获取所有健康的源
func (m *SourceManager) GetHealthy() []*model.Source {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sources []*model.Source
	for _, src := range m.sources {
		if src.Enabled && src.IsHealthy() {
			sources = append(sources, src)
		}
	}
	return sources
}

// GetByCapability 按能力筛选源
func (m *SourceManager) GetByCapability(needFC, needThinking, needVision bool, modelName string) []*model.Source {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sources []*model.Source
	for _, src := range m.sources {
		if !src.Enabled || !src.IsHealthy() {
			continue
		}
		// CPA 特殊处理：不支持 Thinking，FC 按 provider 判断
		if src.Type == model.SourceTypeCPA {
			if modelName != "" {
				provider := src.GetProviderForModel(modelName)
				if provider != "" && !src.IsCPAProviderEnabled(provider) {
					continue
				}
			}
			if needThinking {
				continue // CPA 不支持 Thinking，直接跳过
			}
			if needFC && !src.SupportsFCForModel(modelName) {
				continue
			}
		} else {
			if needFC && !src.Capabilities.FunctionCalling {
				continue
			}
			if needThinking && !src.Capabilities.ExtendedThinking {
				continue
			}
		}
		if needVision && !src.Capabilities.Vision {
			continue
		}
		if modelName != "" && !src.SupportsModel(modelName) {
			continue
		}
		sources = append(sources, src)
	}
	return sources
}

// UpdateStatus 更新源状态
func (m *SourceManager) UpdateStatus(id string, status *model.SourceStatus) {
	m.mu.RLock()
	src, ok := m.sources[id]
	m.mu.RUnlock()

	if ok {
		src.SetStatus(status)
	}
}
