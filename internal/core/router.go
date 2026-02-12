package core

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/xiaopang/fusionapi/internal/model"
)

// 路由策略
const (
	StrategyPriority     = "priority"
	StrategyRoundRobin   = "round-robin"
	StrategyWeighted     = "weighted"
	StrategyLeastLatency = "least-latency"
	StrategyLeastCost    = "least-cost"
)

// 错误定义
var (
	ErrNoAvailableSource = errors.New("no available source")
	ErrSourceNotFound    = errors.New("source not found")
)

// Router 路由器
type Router struct {
	manager    *SourceManager
	strategy   string
	strategyMu sync.RWMutex
	rrIndex    uint64 // round-robin 索引
}

// NewRouter 创建路由器
func NewRouter(manager *SourceManager, strategy string) *Router {
	return &Router{
		manager:  manager,
		strategy: strategy,
	}
}

// getStrategy 获取当前路由策略（线程安全）
func (r *Router) getStrategy() string {
	r.strategyMu.RLock()
	defer r.strategyMu.RUnlock()
	return r.strategy
}

// RouteRequest 为请求选择源
func (r *Router) RouteRequest(req *model.ChatCompletionRequest, exclude []string) (*model.Source, error) {
	needFC := req.HasTools()
	needThinking := req.HasThinking()
	needVision := req.HasVision()

	// 获取符合条件的源
	candidates := r.manager.GetByCapability(
		needFC,
		needThinking,
		needVision,
		req.Model,
	)

	// 如果请求包含工具但没有 FC 候选源，允许降级到非 FC 源
	if len(candidates) == 0 && needFC {
		candidates = r.manager.GetByCapability(
			false,
			needThinking,
			needVision,
			req.Model,
		)
	}

	// 排除已尝试的源
	if len(exclude) > 0 {
		excludeMap := make(map[string]bool)
		for _, id := range exclude {
			excludeMap[id] = true
		}
		var filtered []*model.Source
		for _, src := range candidates {
			if !excludeMap[src.ID] {
				filtered = append(filtered, src)
			}
		}
		candidates = filtered
	}

	// 过滤重试后如果 FC 候选为空，再次尝试降级到非 FC 源
	if len(candidates) == 0 && needFC {
		fallback := r.manager.GetByCapability(
			false,
			needThinking,
			needVision,
			req.Model,
		)
		if len(exclude) > 0 {
			excludeMap := make(map[string]bool)
			for _, id := range exclude {
				excludeMap[id] = true
			}
			for _, src := range fallback {
				if !excludeMap[src.ID] {
					candidates = append(candidates, src)
				}
			}
		} else {
			candidates = fallback
		}
	}

	if len(candidates) == 0 {
		return nil, ErrNoAvailableSource
	}

	// 根据策略选择
	strategy := r.getStrategy()
	switch strategy {
	case StrategyRoundRobin:
		return r.roundRobin(candidates), nil
	case StrategyWeighted:
		return r.weighted(candidates), nil
	case StrategyLeastLatency:
		return r.leastLatency(candidates), nil
	case StrategyLeastCost:
		return r.leastCost(candidates), nil
	default: // priority
		return r.priority(candidates), nil
	}
}

// priority 按优先级选择
func (r *Router) priority(candidates []*model.Source) *model.Source {
	if len(candidates) == 0 {
		return nil
	}

	// 按优先级排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	// 找出优先级相同的源
	topPriority := candidates[0].Priority
	var sameRank []*model.Source
	for _, src := range candidates {
		if src.Priority == topPriority {
			sameRank = append(sameRank, src)
		} else {
			break
		}
	}

	// 优先级相同则轮询
	if len(sameRank) > 1 {
		idx := atomic.AddUint64(&r.rrIndex, 1) % uint64(len(sameRank))
		return sameRank[idx]
	}

	return candidates[0]
}

// roundRobin 轮询
func (r *Router) roundRobin(candidates []*model.Source) *model.Source {
	if len(candidates) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&r.rrIndex, 1) % uint64(len(candidates))
	return candidates[idx]
}

// weighted 按权重选择
func (r *Router) weighted(candidates []*model.Source) *model.Source {
	if len(candidates) == 0 {
		return nil
	}

	// 计算总权重
	var totalWeight int
	for _, src := range candidates {
		w := src.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}

	// 轮询式加权选择
	idx := int(atomic.AddUint64(&r.rrIndex, 1) % uint64(totalWeight))
	var cumulative int
	for _, src := range candidates {
		w := src.Weight
		if w <= 0 {
			w = 1
		}
		cumulative += w
		if idx < cumulative {
			return src
		}
	}

	return candidates[0]
}

// leastLatency 选择延迟最低的源
func (r *Router) leastLatency(candidates []*model.Source) *model.Source {
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		si := candidates[i].GetStatus()
		sj := candidates[j].GetStatus()
		return si.Latency < sj.Latency
	})

	return candidates[0]
}

// leastCost 选择余额最多的源
func (r *Router) leastCost(candidates []*model.Source) *model.Source {
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		si := candidates[i].GetStatus()
		sj := candidates[j].GetStatus()
		return si.Balance > sj.Balance
	})

	return candidates[0]
}

// SetStrategy 设置路由策略（线程安全）
func (r *Router) SetStrategy(strategy string) {
	r.strategyMu.Lock()
	defer r.strategyMu.Unlock()
	r.strategy = strategy
}
