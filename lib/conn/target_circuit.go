package conn

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type TargetCircuitState struct {
	failures  int
	openUntil time.Time
	lastError string
}

type TargetCircuitBreaker struct {
	mu           sync.Mutex
	states       map[string]*TargetCircuitState
	threshold    int
	openDuration time.Duration
	now          func() time.Time
}

type TargetCircuitOpenError struct {
	Target     string
	RetryAfter time.Duration
	LastError  string
}

func NewTargetCircuitBreaker(threshold int, openDuration time.Duration) *TargetCircuitBreaker {
	if threshold < 1 {
		threshold = 1
	}
	if openDuration < 0 {
		openDuration = 0
	}
	return &TargetCircuitBreaker{
		states:       make(map[string]*TargetCircuitState),
		threshold:    threshold,
		openDuration: openDuration,
		now:          time.Now,
	}
}

func (b *TargetCircuitBreaker) BeforeDial(connType string, targetHost string) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.states[targetCircuitKey(connType, targetHost)]
	if state == nil || state.openUntil.IsZero() {
		return nil
	}
	now := b.now()
	if !state.openUntil.After(now) {
		state.failures = 0
		state.openUntil = time.Time{}
		state.lastError = ""
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
	state := b.states[key]
	if state == nil {
		state = &TargetCircuitState{}
		b.states[key] = state
	}
	state.failures++
	state.lastError = err.Error()
	if state.failures >= b.threshold {
		state.openUntil = b.now().Add(b.openDuration)
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
