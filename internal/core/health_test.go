package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaopang/fusionapi/internal/config"
	"github.com/xiaopang/fusionapi/internal/model"
)

func TestHealthChecker_ProbeSource_SetsAuthHeader(t *testing.T) {
	tests := []struct {
		name         string
		src          *model.Source
		wantHeader   string
		wantContains string
	}{
		{
			name: "openai uses Authorization Bearer",
			src:  &model.Source{Type: model.SourceTypeOpenAI, APIKey: "sk-test"},
			wantHeader:   "Authorization",
			wantContains: "Bearer sk-test",
		},
		{
			name: "anthropic uses x-api-key",
			src:  &model.Source{Type: model.SourceTypeAnthropic, APIKey: "ak-test"},
			wantHeader:   "x-api-key",
			wantContains: "ak-test",
		},
		{
			name: "cpa without api key sends no Authorization",
			src:  &model.Source{Type: model.SourceTypeCPA, APIKey: ""},
			wantHeader:   "Authorization",
			wantContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			var gotXKey string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				gotXKey = r.Header.Get("x-api-key")
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"data":[]}`)
			}))
			defer srv.Close()

			src := *tt.src
			src.BaseURL = srv.URL
			src.Enabled = true

			mgr := &SourceManager{sources: map[string]*model.Source{"s1": &src}}
			hc := NewHealthChecker(mgr, &config.HealthCheckConfig{Enabled: true, Interval: 60, Timeout: 2, FailureThreshold: 1})

			err := hc.TestConnection(&src)
			if err != nil {
				t.Fatalf("TestConnection failed: %v", err)
			}

			switch tt.wantHeader {
			case "Authorization":
				if tt.wantContains == "" {
					if gotAuth != "" {
						t.Fatalf("expected no Authorization header, got %q", gotAuth)
					}
				} else if gotAuth != tt.wantContains {
					t.Fatalf("expected Authorization=%q, got %q", tt.wantContains, gotAuth)
				}
			case "x-api-key":
				if gotXKey != tt.wantContains {
					t.Fatalf("expected x-api-key=%q, got %q", tt.wantContains, gotXKey)
				}
			}
		})
	}
}

func TestHealthChecker_CheckSource_SetsUnhealthyAfterThreshold(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer srv.Close()

	src := &model.Source{ID: "s1", Name: "S1", Type: model.SourceTypeOpenAI, BaseURL: srv.URL, APIKey: "k", Enabled: true}
	src.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy})

	mgr := &SourceManager{sources: map[string]*model.Source{"s1": src}}
	hc := NewHealthChecker(mgr, &config.HealthCheckConfig{Enabled: true, Interval: 60, Timeout: 2, FailureThreshold: 2})

	// fail #1 -> still healthy
	hc.checkSource(src)
	st := src.GetStatus()
	if st.State != model.HealthStateHealthy {
		t.Fatalf("expected healthy after first failure, got %s", st.State)
	}
	// fail #2 -> unhealthy
	hc.checkSource(src)
	st = src.GetStatus()
	if st.State != model.HealthStateUnhealthy {
		t.Fatalf("expected unhealthy after reaching threshold, got %s", st.State)
	}
	if st.ErrorCount < 2 {
		t.Fatalf("expected ErrorCount>=2, got %d", st.ErrorCount)
	}
	if st.LastError == "" {
		t.Fatalf("expected LastError to be set")
	}
	if reqCount.Load() < 2 {
		t.Fatalf("expected at least 2 probe requests, got %d", reqCount.Load())
	}
	if st.Latency <= 0 {
		t.Fatalf("expected latency to be set")
	}
	if st.LastCheck.IsZero() {
		t.Fatalf("expected LastCheck to be set")
	}
}

func TestHealthChecker_UpdateConfig_RestartsWithoutDuplicateRuns(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()

	src := &model.Source{ID: "s1", Name: "S1", Type: model.SourceTypeOpenAI, BaseURL: srv.URL, APIKey: "k", Enabled: true}
	src.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy})

	mgr := &SourceManager{sources: map[string]*model.Source{"s1": src}}
	cfg := &config.HealthCheckConfig{Enabled: true, Interval: 1, Timeout: 2, FailureThreshold: 1}
	hc := NewHealthChecker(mgr, cfg)
	// Keep tests fast: allow at most a few ticks.
	hc.Start()
	defer hc.Stop()

	time.Sleep(1200 * time.Millisecond) // allow initial + ~1 tick
	before := reqCount.Load()
	if before < 1 {
		t.Fatalf("expected at least 1 request, got %d", before)
	}

	// Update config with same values should not restart or create new run.
	hc.UpdateConfig(&config.HealthCheckConfig{Enabled: true, Interval: 1, Timeout: 2, FailureThreshold: 1})

	// Now change interval - should restart. Ensure we still only have one run loop.
	hc.UpdateConfig(&config.HealthCheckConfig{Enabled: true, Interval: 2, Timeout: 2, FailureThreshold: 1})

	time.Sleep(1200 * time.Millisecond)
	after := reqCount.Load()
	if after-before > 5 {
		// If duplicate loops exist, requests will spike noticeably for 1s interval.
		t.Fatalf("too many requests; possible duplicate loops: before=%d after=%d", before, after)
	}
}

func TestHealthChecker_CPAAutoDetect_SingleRequest(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[
			{"id":"gemini-2.0-flash","provider":"gemini"},
			{"id":"claude-3.5-sonnet","provider":"claude"}
		]}`)
	}))
	defer srv.Close()

	src := &model.Source{
		ID: "cpa1", Name: "CPA", Type: model.SourceTypeCPA,
		BaseURL: srv.URL, Enabled: true,
		CPA: &model.CPAConfig{AutoDetect: true, Providers: []string{"gemini", "claude"}, AccountMode: "multi"},
	}
	src.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy})

	mgr := &SourceManager{sources: map[string]*model.Source{"cpa1": src}}
	hc := NewHealthChecker(mgr, &config.HealthCheckConfig{Enabled: true, Interval: 60, Timeout: 2, FailureThreshold: 3})

	hc.checkSource(src)

	// Must be exactly 1 HTTP request (probe + detect combined)
	if n := reqCount.Load(); n != 1 {
		t.Fatalf("expected exactly 1 request for CPA autodetect, got %d", n)
	}

	// Status should be healthy
	st := src.GetStatus()
	if st.State != model.HealthStateHealthy {
		t.Fatalf("expected healthy, got %s", st.State)
	}

	// Models should be detected
	if len(src.Capabilities.Models) != 2 {
		t.Fatalf("expected 2 detected models, got %d", len(src.Capabilities.Models))
	}

	// FC and Vision should be set from provider capabilities
	if !src.Capabilities.FunctionCalling {
		t.Fatal("expected FunctionCalling=true")
	}
	if !src.Capabilities.Vision {
		t.Fatal("expected Vision=true")
	}

	// ModelProviders map should be populated
	if st.ModelProviders == nil || len(st.ModelProviders) != 2 {
		t.Fatalf("expected 2 model providers, got %v", st.ModelProviders)
	}
}

func TestHealthChecker_NonCPA_SingleRequest(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"gpt-4"}]}`)
	}))
	defer srv.Close()

	src := &model.Source{
		ID: "s1", Name: "OpenAI", Type: model.SourceTypeOpenAI,
		BaseURL: srv.URL, APIKey: "sk-test", Enabled: true,
	}
	src.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy})

	mgr := &SourceManager{sources: map[string]*model.Source{"s1": src}}
	hc := NewHealthChecker(mgr, &config.HealthCheckConfig{Enabled: true, Interval: 60, Timeout: 2, FailureThreshold: 3})

	hc.checkSource(src)

	if n := reqCount.Load(); n != 1 {
		t.Fatalf("expected exactly 1 request for non-CPA probe, got %d", n)
	}
	if src.GetStatus().State != model.HealthStateHealthy {
		t.Fatal("expected healthy")
	}
}

func TestHealthChecker_RecoveryAfterFailure(t *testing.T) {
	callNum := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callNum.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "down")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()

	src := &model.Source{ID: "s1", Name: "S1", Type: model.SourceTypeOpenAI, BaseURL: srv.URL, APIKey: "k", Enabled: true}
	src.SetStatus(&model.SourceStatus{State: model.HealthStateHealthy})

	mgr := &SourceManager{sources: map[string]*model.Source{"s1": src}}
	hc := NewHealthChecker(mgr, &config.HealthCheckConfig{Enabled: true, Interval: 60, Timeout: 2, FailureThreshold: 2})

	hc.checkSource(src) // fail 1
	hc.checkSource(src) // fail 2 -> unhealthy
	if src.GetStatus().State != model.HealthStateUnhealthy {
		t.Fatal("expected unhealthy")
	}

	hc.checkSource(src) // success -> should recover
	st := src.GetStatus()
	if st.State != model.HealthStateHealthy {
		t.Fatalf("expected recovery to healthy, got %s", st.State)
	}
	if st.ConsecutiveFail != 0 {
		t.Fatalf("expected ConsecutiveFail=0 after recovery, got %d", st.ConsecutiveFail)
	}
}
