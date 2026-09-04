package conn

import (
	"errors"
	"testing"
	"time"
)

func TestTargetCircuitBreakerAdaptiveOpenDuration(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(2, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time {
		return now
	}

	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("target should not be open before threshold: %v", err)
	}
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 5*time.Second)

	now = now.Add(5*time.Second + time.Millisecond)
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("first half-open probe should be allowed: %v", err)
	}
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err == nil {
		t.Fatal("second half-open probe should fast-fail while the first probe is in flight")
	}
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 10*time.Second)

	now = now.Add(10*time.Second + time.Millisecond)
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("half-open probe after second window should be allowed: %v", err)
	}
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 20*time.Second)

	now = now.Add(20*time.Second + time.Millisecond)
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("half-open probe after third window should be allowed: %v", err)
	}
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 30*time.Second)

	now = now.Add(30*time.Second + time.Millisecond)
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("half-open probe after capped window should be allowed: %v", err)
	}
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 30*time.Second)
}

func TestTargetCircuitBreakerFailureWindow(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(3, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time {
		return now
	}

	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	now = now.Add(11 * time.Second)
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	now = now.Add(time.Second)
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("failures older than the 10-second window should not open the circuit: %v", err)
	}

	now = now.Add(time.Second)
	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 5*time.Second)
}

func TestTargetCircuitBreakerAllOpenDoesNotConsumeHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(1, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time {
		return now
	}

	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	now = now.Add(5*time.Second + time.Millisecond)

	allOpen, err := breaker.AllOpen("tcp", []string{"127.0.0.1:8080"})
	if allOpen || err != nil {
		t.Fatalf("expired open window should not be considered all-open, allOpen=%t err=%v", allOpen, err)
	}
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("all-open check should not consume the half-open probe: %v", err)
	}
}

func TestTargetCircuitBreakerClosesOnHalfOpenSuccess(t *testing.T) {
	now := time.Date(2026, 9, 4, 11, 0, 0, 0, time.Local)
	breaker := NewTargetCircuitBreaker(1, 5*time.Second, 30*time.Second)
	breaker.now = func() time.Time {
		return now
	}

	breaker.AfterDial("tcp", "127.0.0.1:8080", errors.New("connection refused"))
	assertTargetFastFailRetryAfter(t, breaker, "127.0.0.1:8080", 5*time.Second)

	now = now.Add(5*time.Second + time.Millisecond)
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("half-open probe should be allowed: %v", err)
	}
	breaker.AfterDial("tcp", "127.0.0.1:8080", nil)
	if err := breaker.BeforeDial("tcp", "127.0.0.1:8080"); err != nil {
		t.Fatalf("target should close immediately after a successful half-open probe: %v", err)
	}
}

func assertTargetFastFailRetryAfter(t *testing.T, breaker *TargetCircuitBreaker, target string, want time.Duration) {
	t.Helper()
	err := breaker.BeforeDial("tcp", target)
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
