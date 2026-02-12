package core

import (
	"testing"
	"time"

	"github.com/xiaopang/fusionapi/internal/model"
)

// helper to create a healthy source with given fields
func newTestSource(id string, priority, weight int, models []string) *model.Source {
	return &model.Source{
		ID:       id,
		Name:     id,
		Type:     model.SourceTypeOpenAI,
		Enabled:  true,
		Priority: priority,
		Weight:   weight,
		Capabilities: model.Capabilities{
			Models: models,
		},
		Status: &model.SourceStatus{
			State: model.HealthStateHealthy,
		},
	}
}

func setupManager(sources ...*model.Source) *SourceManager {
	m := &SourceManager{
		sources: make(map[string]*model.Source),
	}
	for _, s := range sources {
		m.sources[s.ID] = s
	}
	return m
}

// --------- Priority strategy ---------

func TestRouter_Priority_SelectsLowestNumber(t *testing.T) {
	s1 := newTestSource("s1", 10, 1, []string{"gpt-4"})
	s2 := newTestSource("s2", 1, 1, []string{"gpt-4"})
	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyPriority)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}

	got, err := r.RouteRequest(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "s2" {
		t.Fatalf("expected s2 (priority=1), got %s", got.ID)
	}
}

func TestRouter_Priority_Exclude(t *testing.T) {
	s1 := newTestSource("s1", 1, 1, []string{"gpt-4"})
	s2 := newTestSource("s2", 2, 1, []string{"gpt-4"})
	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyPriority)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}
	got, err := r.RouteRequest(req, []string{"s1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "s2" {
		t.Fatalf("expected s2 (s1 excluded), got %s", got.ID)
	}
}

// --------- Round-robin strategy ---------

func TestRouter_RoundRobin(t *testing.T) {
	s1 := newTestSource("s1", 1, 1, []string{"gpt-4"})
	s2 := newTestSource("s2", 1, 1, []string{"gpt-4"})
	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyRoundRobin)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}

	seen := map[string]int{}
	for i := 0; i < 10; i++ {
		got, err := r.RouteRequest(req, nil)
		if err != nil {
			t.Fatal(err)
		}
		seen[got.ID]++
	}

	if len(seen) < 2 {
		t.Fatalf("round-robin should distribute across sources, got: %v", seen)
	}
	// Both should be selected roughly equally
	if seen["s1"] == 0 || seen["s2"] == 0 {
		t.Fatalf("expected both sources to be selected, got: %v", seen)
	}
}

// --------- Least-latency strategy ---------

func TestRouter_LeastLatency(t *testing.T) {
	s1 := newTestSource("s1", 1, 1, []string{"gpt-4"})
	s1.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy, Latency: 200 * time.Millisecond})

	s2 := newTestSource("s2", 1, 1, []string{"gpt-4"})
	s2.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy, Latency: 50 * time.Millisecond})

	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyLeastLatency)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}
	got, err := r.RouteRequest(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "s2" {
		t.Fatalf("expected s2 (lower latency), got %s", got.ID)
	}
}

// --------- Least-cost strategy ---------

func TestRouter_LeastCost(t *testing.T) {
	s1 := newTestSource("s1", 1, 1, []string{"gpt-4"})
	s1.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy, Balance: 5.0})

	s2 := newTestSource("s2", 1, 1, []string{"gpt-4"})
	s2.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy, Balance: 50.0})

	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyLeastCost)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}
	got, err := r.RouteRequest(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "s2" {
		t.Fatalf("expected s2 (higher balance), got %s", got.ID)
	}
}

// --------- No available source ---------

func TestRouter_NoAvailableSource(t *testing.T) {
	mgr := setupManager() // empty
	r := NewRouter(mgr, StrategyPriority)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}
	_, err := r.RouteRequest(req, nil)
	if err != ErrNoAvailableSource {
		t.Fatalf("expected ErrNoAvailableSource, got %v", err)
	}
}

// --------- Weighted strategy ---------

func TestRouter_Weighted(t *testing.T) {
	s1 := newTestSource("s1", 1, 9, []string{"gpt-4"})
	s2 := newTestSource("s2", 1, 1, []string{"gpt-4"})
	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyWeighted)

	req := &model.ChatCompletionRequest{Model: "gpt-4"}

	seen := map[string]int{}
	for i := 0; i < 100; i++ {
		got, err := r.RouteRequest(req, nil)
		if err != nil {
			t.Fatal(err)
		}
		seen[got.ID]++
	}

	// s1 (weight=9) should be selected ~90 times, s2 (weight=1) ~10 times.
	// We just verify s1 gets the majority.
	if seen["s1"] <= seen["s2"] {
		t.Fatalf("s1 (weight=9) should be picked more than s2 (weight=1), got: %v", seen)
	}
}

// --------- Model filtering ---------

func TestRouter_ModelFiltering(t *testing.T) {
	s1 := newTestSource("s1", 1, 1, []string{"gpt-4"})
	s2 := newTestSource("s2", 1, 1, []string{"claude-3"})
	mgr := setupManager(s1, s2)
	r := NewRouter(mgr, StrategyPriority)

	req := &model.ChatCompletionRequest{Model: "claude-3"}
	got, err := r.RouteRequest(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "s2" {
		t.Fatalf("expected s2 (supports claude-3), got %s", got.ID)
	}
}
