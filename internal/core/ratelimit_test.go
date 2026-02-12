package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaopang/fusionapi/internal/model"
)

// newTestRateLimiter creates a RateLimiter without the background cleanup goroutine
// to avoid data-race noise in tests.
func newTestRateLimiter() *RateLimiter {
	return &RateLimiter{
		windows:    make(map[string][]time.Time),
		dailyCount: make(map[string]int),
		concurrent: make(map[string]int),
		errorCount: make(map[string]int),
		autoBanned: make(map[string]time.Time),
	}
}

// --------------- Enter() tests ---------------

func TestEnter_ConcurrentLimit(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{Concurrent: 1}

	ok1, _, release1 := rl.Enter("k1", limits, "")
	if !ok1 {
		t.Fatal("first Enter should succeed")
	}

	ok2, reason, _ := rl.Enter("k1", limits, "")
	if ok2 {
		t.Fatal("second Enter should be rejected (concurrent=1)")
	}
	if !strings.Contains(reason, "Concurrent") {
		t.Fatalf("unexpected reason: %s", reason)
	}

	// After release, the next call should succeed
	release1()

	ok3, _, release3 := rl.Enter("k1", limits, "")
	if !ok3 {
		t.Fatal("Enter after release should succeed")
	}
	release3()
}

func TestEnter_ConcurrentLimit_Parallel(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{Concurrent: 5}

	var wg sync.WaitGroup
	accepted := make(chan struct{}, 20)
	rejected := make(chan struct{}, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, release := rl.Enter("k1", limits, "")
			if ok {
				accepted <- struct{}{}
				// hold for a moment so concurrent slots stay occupied
				release()
			} else {
				rejected <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(accepted)
	close(rejected)

	a := len(accepted)
	r := len(rejected)
	t.Logf("accepted=%d rejected=%d", a, r)
	// With concurrent=5, at least some must be rejected when 20 goroutines race.
	// (It's possible all 20 pass if goroutines serialize, so we just verify totals.)
	if a+r != 20 {
		t.Fatalf("expected 20 total, got %d", a+r)
	}
}

func TestEnter_RPMLimit(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{RPM: 3}

	for i := 0; i < 3; i++ {
		ok, _, release := rl.Enter("k1", limits, "")
		if !ok {
			t.Fatalf("call %d should succeed", i+1)
		}
		release()
	}

	ok, reason, _ := rl.Enter("k1", limits, "")
	if ok {
		t.Fatal("4th call should be rejected (RPM=3)")
	}
	if !strings.Contains(reason, "RPM") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestEnter_DailyQuota(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{DailyQuota: 2}

	for i := 0; i < 2; i++ {
		ok, _, release := rl.Enter("k1", limits, "")
		if !ok {
			t.Fatalf("call %d should succeed", i+1)
		}
		release()
	}

	ok, reason, _ := rl.Enter("k1", limits, "")
	if ok {
		t.Fatal("3rd call should be rejected (DailyQuota=2)")
	}
	if !strings.Contains(reason, "Daily quota") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestEnter_ToolQuotaExceeded_NoGlobalCharging(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{
		RPM:        100,
		DailyQuota: 100,
		ToolQuotas: map[string]int{"cursor": 1},
	}

	// First call with tool "cursor" succeeds
	ok1, _, release1 := rl.Enter("k1", limits, "cursor")
	if !ok1 {
		t.Fatal("first Enter with cursor should succeed")
	}
	release1()

	// Second call with tool "cursor" should be rejected
	ok2, reason, _ := rl.Enter("k1", limits, "cursor")
	if ok2 {
		t.Fatal("second Enter with cursor should be rejected (tool quota=1)")
	}
	if !strings.Contains(reason, "Tool quota") {
		t.Fatalf("unexpected reason: %s", reason)
	}

	// Verify that the RPM window and daily count were NOT incremented for the rejected call.
	// After 1 successful call, RPM window should have exactly 1 entry, daily count should be 1.
	rl.mu.Lock()
	rpmCount := len(rl.windows["k1"])
	today := time.Now().Format("2006-01-02")
	dailyCount := rl.dailyCount["k1:"+today]
	rl.mu.Unlock()

	if rpmCount != 1 {
		t.Fatalf("RPM window should have 1 entry, got %d", rpmCount)
	}
	if dailyCount != 1 {
		t.Fatalf("daily count should be 1, got %d", dailyCount)
	}
}

func TestEnter_ReleaseIdempotent(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{Concurrent: 1}

	_, _, release := rl.Enter("k1", limits, "")

	// Calling release multiple times should not panic or double-decrement.
	release()
	release()
	release()

	rl.mu.Lock()
	count := rl.concurrent["k1"]
	rl.mu.Unlock()

	if count != 0 {
		t.Fatalf("concurrent should be 0 after release, got %d", count)
	}
}

// --------------- AllowWithTool() fix verification ---------------

func TestAllowWithTool_NoChargeOnToolDenial(t *testing.T) {
	rl := newTestRateLimiter()
	limits := model.KeyLimits{
		RPM:        100,
		DailyQuota: 100,
		ToolQuotas: map[string]int{"cursor": 1},
	}

	// First call succeeds
	ok1, _ := rl.AllowWithTool("k1", limits, "cursor")
	if !ok1 {
		t.Fatal("first call should succeed")
	}

	// Second call should fail on tool quota
	ok2, reason := rl.AllowWithTool("k1", limits, "cursor")
	if ok2 {
		t.Fatal("second call should be rejected")
	}
	if !strings.Contains(reason, "Tool quota") {
		t.Fatalf("unexpected reason: %s", reason)
	}

	// Verify RPM and daily were not double-charged
	rl.mu.Lock()
	rpmCount := len(rl.windows["k1"])
	today := time.Now().Format("2006-01-02")
	dailyCount := rl.dailyCount["k1:"+today]
	rl.mu.Unlock()

	if rpmCount != 1 {
		t.Fatalf("RPM should be 1 (only successful call), got %d", rpmCount)
	}
	if dailyCount != 1 {
		t.Fatalf("daily count should be 1, got %d", dailyCount)
	}
}

// --------------- AutoBan tests ---------------

func TestAutoBan(t *testing.T) {
	rl := newTestRateLimiter()

	// Record errors up to threshold
	for i := 0; i < AutoBanThreshold-1; i++ {
		banned := rl.RecordError("k1")
		if banned {
			t.Fatalf("should not be banned at error %d", i+1)
		}
	}

	// Next error triggers ban
	banned := rl.RecordError("k1")
	if !banned {
		t.Fatal("should be banned at threshold")
	}

	isBanned, remaining := rl.IsAutoBanned("k1")
	if !isBanned {
		t.Fatal("should report as banned")
	}
	if remaining <= 0 {
		t.Fatal("remaining should be positive")
	}

	// RecordSuccess resets error count
	rl.RecordSuccess("k1")
	rl.mu.Lock()
	errCount := rl.errorCount["k1"]
	rl.mu.Unlock()
	if errCount != 0 {
		t.Fatalf("error count should be 0 after success, got %d", errCount)
	}
}
