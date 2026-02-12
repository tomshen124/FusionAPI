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
