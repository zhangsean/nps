package conn

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestTargetCircuitBreakerAdaptiveOpenDuration(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(2, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time { return now }
	target := "127.0.0.1:8080"

	failTargetDial(t, breaker, target, errors.New("connection refused"))
	permit := allowTargetDial(t, breaker, target)
	breaker.AfterDial(permit, errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, target, 5*time.Second)

	windows := []time.Duration{10 * time.Second, 20 * time.Second, 30 * time.Second, 30 * time.Second}
	for _, want := range windows {
		now = now.Add(currentRetryAfter(t, breaker, target) + time.Millisecond)
		permit = allowTargetDial(t, breaker, target)
		if _, err := breaker.BeforeDial("tcp", target); err == nil {
			t.Fatal("second half-open probe should fast-fail while the first probe is in flight")
		}
		breaker.AfterDial(permit, errors.New("connection refused"))
		assertTargetFastFailRetryAfter(t, breaker, target, want)
	}
}

func TestTargetCircuitBreakerFailureWindow(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(3, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time { return now }
	target := "127.0.0.1:8080"

	failTargetDial(t, breaker, target, errors.New("connection refused"))
	now = now.Add(11 * time.Second)
	failTargetDial(t, breaker, target, errors.New("connection refused"))
	now = now.Add(time.Second)
	failTargetDial(t, breaker, target, errors.New("connection refused"))
	permit := allowTargetDial(t, breaker, target)

	now = now.Add(time.Second)
	breaker.AfterDial(permit, errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, target, 5*time.Second)
}

func TestTargetCircuitBreakerAllOpenDoesNotConsumeHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(1, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time { return now }
	target := "127.0.0.1:8080"

	failTargetDial(t, breaker, target, errors.New("connection refused"))
	now = now.Add(5*time.Second + time.Millisecond)

	allOpen, err := breaker.AllOpen("tcp", []string{target})
	if allOpen || err != nil {
		t.Fatalf("expired open window should not be considered all-open, allOpen=%t err=%v", allOpen, err)
	}
	allowTargetDial(t, breaker, target)
}

func TestTargetCircuitBreakerClosesOnHalfOpenSuccess(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(1, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time { return now }
	target := "127.0.0.1:8080"

	failTargetDial(t, breaker, target, errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, target, 5*time.Second)

	now = now.Add(5*time.Second + time.Millisecond)
	permit := allowTargetDial(t, breaker, target)
	breaker.AfterDial(permit, nil)
	allowTargetDial(t, breaker, target)

	metric := targetMetric(t, breaker, target)
	if metric.State != "closed" || metric.Failures != 0 || metric.DialSuccessTotal != 1 || metric.DialFailureTotal != 1 {
		t.Fatalf("unexpected metric after recovery: %+v", metric)
	}
}

func TestTargetCircuitBreakerStaleSuccessCannotCloseNewOpenGeneration(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(1, 5*time.Second)
	breaker.now = func() time.Time { return now }
	target := "127.0.0.1:8080"

	stalePermit := allowTargetDial(t, breaker, target)
	failingPermit := allowTargetDial(t, breaker, target)
	breaker.AfterDial(failingPermit, errors.New("connection refused"))
	breaker.AfterDial(stalePermit, nil)

	assertTargetFastFailRetryAfter(t, breaker, target, 5*time.Second)
	metric := targetMetric(t, breaker, target)
	if metric.State != "open" || metric.DialSuccessTotal != 1 || metric.DialFailureTotal != 1 || metric.CircuitOpenTotal != 1 {
		t.Fatalf("stale result changed current circuit generation: %+v", metric)
	}
}

func TestTargetCircuitBreakerMetrics(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(2, 5*time.Second)
	breaker.now = func() time.Time { return now }
	target := "127.0.0.1:8080"

	failTargetDial(t, breaker, target, &net.DNSError{Err: "timeout", IsTimeout: true})
	failTargetDial(t, breaker, target, errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, target, 5*time.Second)

	metric := targetMetric(t, breaker, target)
	if metric.State != "open" || metric.Failures != 2 || metric.DialsInFlight != 0 || metric.RetryAfterMS != 5000 {
		t.Fatalf("unexpected target state metric: %+v", metric)
	}
	if metric.DialFailureTotal != 2 || metric.DialTimeoutTotal != 1 || metric.FastFailTotal != 1 || metric.CircuitOpenTotal != 1 {
		t.Fatalf("unexpected target counter metric: %+v", metric)
	}
}

func TestTargetCircuitBreakerReclaimsExpiredIdleState(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(2, 5*time.Second)
	breaker.now = func() time.Time { return now }
	breaker.stateTTL = time.Minute
	target := "127.0.0.1:8080"

	permit := allowTargetDial(t, breaker, target)
	breaker.AfterDial(permit, nil)
	now = now.Add(time.Minute)
	if metrics := breaker.Metrics(); len(metrics) != 0 {
		t.Fatalf("expired idle state was not reclaimed: %+v", metrics)
	}
}

func TestTargetCircuitBreakerCapacityEvictsOldestIdleState(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(2, 5*time.Second)
	breaker.now = func() time.Time { return now }
	breaker.maxStates = 2
	breaker.stateTTL = time.Hour

	for _, target := range []string{"127.0.0.1:8001", "127.0.0.1:8002"} {
		permit := allowTargetDial(t, breaker, target)
		breaker.AfterDial(permit, nil)
		now = now.Add(time.Second)
	}
	allowTargetDial(t, breaker, "127.0.0.1:8003")

	if _, ok := breaker.states[targetCircuitKey("tcp", "127.0.0.1:8001")]; ok {
		t.Fatal("oldest idle state should have been evicted")
	}
	if len(breaker.states) != 2 {
		t.Fatalf("unexpected state count %d", len(breaker.states))
	}
}

func TestTargetCircuitBreakerDoesNotEvictActiveOrOpenState(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(1, 5*time.Second)
	breaker.now = func() time.Time { return now }
	breaker.maxStates = 1

	activePermit := allowTargetDial(t, breaker, "127.0.0.1:8001")
	allowTargetDial(t, breaker, "127.0.0.1:8002")
	if _, ok := breaker.states[activePermit.key]; !ok {
		t.Fatal("active state must not be evicted")
	}

	openBreaker := NewTargetCircuitBreaker(1, 5*time.Second)
	openBreaker.now = func() time.Time { return now }
	openBreaker.maxStates = 1
	failTargetDial(t, openBreaker, "127.0.0.1:9001", errors.New("connection refused"))
	allowTargetDial(t, openBreaker, "127.0.0.1:9002")
	if _, ok := openBreaker.states[targetCircuitKey("tcp", "127.0.0.1:9001")]; !ok {
		t.Fatal("open state must not be evicted")
	}
}

func allowTargetDial(t *testing.T, breaker *TargetCircuitBreaker, target string) *TargetDialPermit {
	t.Helper()
	permit, err := breaker.BeforeDial("tcp", target)
	if err != nil {
		t.Fatalf("target %s should allow dial: %v", target, err)
	}
	return permit
}

func failTargetDial(t *testing.T, breaker *TargetCircuitBreaker, target string, dialErr error) {
	t.Helper()
	permit := allowTargetDial(t, breaker, target)
	breaker.AfterDial(permit, dialErr)
}

func assertTargetFastFailRetryAfter(t *testing.T, breaker *TargetCircuitBreaker, target string, want time.Duration) {
	t.Helper()
	_, err := breaker.BeforeDial("tcp", target)
	if err == nil {
		t.Fatal("expected target to fast-fail")
	}
	got, ok := TargetFastFailRetryAfter(err)
	if !ok {
		t.Fatalf("expected fast-fail error, got %T: %v", err, err)
	}
	if got != want {
		t.Fatalf("unexpected retry-after %s, want %s", got, want)
	}
}

func currentRetryAfter(t *testing.T, breaker *TargetCircuitBreaker, target string) time.Duration {
	t.Helper()
	_, err := breaker.BeforeDial("tcp", target)
	if err == nil {
		t.Fatal("expected target to fast-fail")
	}
	retryAfter, ok := TargetFastFailRetryAfter(err)
	if !ok {
		t.Fatalf("expected fast-fail error, got %T: %v", err, err)
	}
	return retryAfter
}

func targetMetric(t *testing.T, breaker *TargetCircuitBreaker, target string) TargetCircuitMetric {
	t.Helper()
	for _, metric := range breaker.Metrics() {
		if metric.Target == target {
			return metric
		}
	}
	t.Fatalf("metric for %s not found", target)
	return TargetCircuitMetric{}
}
