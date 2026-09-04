package conn

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type TargetCircuitState struct {
	failures      int
	openUntil     time.Time
	lastError     string
	openDuration  time.Duration
	probeInFlight bool
}

type TargetCircuitBreaker struct {
	mu                  sync.Mutex
	states              map[string]*TargetCircuitState
	threshold           int
	initialOpenDuration time.Duration
	maxOpenDuration     time.Duration
	now                 func() time.Time
}

type TargetCircuitOpenError struct {
	Target     string
	RetryAfter time.Duration
	LastError  string
}

func NewTargetCircuitBreaker(threshold int, initialOpenDuration time.Duration, maxOpenDuration ...time.Duration) *TargetCircuitBreaker {
	if threshold < 1 {
		threshold = 1
	}
	if initialOpenDuration < 0 {
		initialOpenDuration = 0
	}
	maxDuration := initialOpenDuration
	if len(maxOpenDuration) > 0 {
		maxDuration = maxOpenDuration[0]
	}
	if maxDuration < initialOpenDuration {
		maxDuration = initialOpenDuration
	}
	return &TargetCircuitBreaker{
		states:              make(map[string]*TargetCircuitState),
		threshold:           threshold,
		initialOpenDuration: initialOpenDuration,
		maxOpenDuration:     maxDuration,
		now:                 time.Now,
	}
}

func (b *TargetCircuitBreaker) BeforeDial(connType string, targetHost string) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.states[targetCircuitKey(connType, targetHost)]
	if state == nil {
		return nil
	}
	if state.probeInFlight {
		return &TargetCircuitOpenError{
			Target:     targetHost,
			RetryAfter: time.Millisecond,
			LastError:  state.lastError,
		}
	}
	if state.openUntil.IsZero() {
		return nil
	}
	now := b.now()
	if !state.openUntil.After(now) {
		state.openUntil = time.Time{}
		state.probeInFlight = true
		return nil
	}
	return &TargetCircuitOpenError{
		Target:     targetHost,
		RetryAfter: state.openUntil.Sub(now),
		LastError:  state.lastError,
	}
}

func (b *TargetCircuitBreaker) AfterDial(connType string, targetHost string, err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := targetCircuitKey(connType, targetHost)
	if err == nil {
		delete(b.states, key)
		return
	}
	now := b.now()
	state := b.states[key]
	if state == nil {
		state = &TargetCircuitState{}
		b.states[key] = state
	}
	if state.openUntil.After(now) && !state.probeInFlight {
		state.lastError = err.Error()
		return
	}
	state.probeInFlight = false
	state.failures++
	state.lastError = err.Error()
	if state.failures >= b.threshold {
		duration := state.nextOpenDuration(b.initialOpenDuration, b.maxOpenDuration)
		state.openDuration = duration
		state.openUntil = now.Add(duration)
	}
}

func (b *TargetCircuitBreaker) AllOpen(connType string, targetHosts []string) (bool, error) {
	if b == nil || len(targetHosts) == 0 {
		return false, nil
	}
	var lastErr error
	for _, targetHost := range targetHosts {
		err := b.BeforeDial(connType, targetHost)
		if err == nil {
			return false, nil
		}
		lastErr = err
	}
	return true, lastErr
}

func (e *TargetCircuitOpenError) Error() string {
	target := strings.TrimSpace(e.Target)
	if target == "" {
		target = "unknown"
	}
	message := fmt.Sprintf("target %s temporarily isolated after repeated connect failures", target)
	if e.RetryAfter > 0 {
		message += fmt.Sprintf(", retry after %s", e.RetryAfter.Round(time.Millisecond))
	}
	if e.LastError != "" {
		message += ": " + e.LastError
	}
	return message
}

func TargetFastFailRetryAfter(err error) (time.Duration, bool) {
	var fastFailErr *TargetCircuitOpenError
	if !errors.As(err, &fastFailErr) {
		return 0, false
	}
	return fastFailErr.RetryAfter, true
}

func targetCircuitKey(connType string, targetHost string) string {
	return connType + "\x00" + targetHost
}

func (s *TargetCircuitState) nextOpenDuration(initialDuration time.Duration, maxDuration time.Duration) time.Duration {
	if s.openDuration <= 0 {
		return initialDuration
	}
	next := s.openDuration * 2
	if next < s.openDuration {
		next = maxDuration
	}
	if next > maxDuration {
		next = maxDuration
	}
	return next
}
