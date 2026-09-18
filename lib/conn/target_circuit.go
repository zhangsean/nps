package conn

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultTargetCircuitFailureWindow   = 10 * time.Second
	defaultTargetCircuitStateTTL        = 15 * time.Minute
	defaultTargetCircuitCleanupInterval = time.Minute
	defaultTargetCircuitMaxStates       = 4096
)

type TargetCircuitState struct {
	connType           string
	targetHost         string
	failures           int
	failureWindowStart time.Time
	openUntil          time.Time
	lastError          string
	openDuration       time.Duration
	probeInFlight      bool
	generation         uint64
	lastTouched        time.Time
	dialsInFlight      int64
	dialSuccessTotal   uint64
	dialFailureTotal   uint64
	dialTimeoutTotal   uint64
	fastFailTotal      uint64
	circuitOpenTotal   uint64
}

type TargetDialPermit struct {
	key        string
	generation uint64
}

type TargetCircuitMetric struct {
	ConnType         string `json:"conn_type"`
	Target           string `json:"target"`
	State            string `json:"state"`
	Failures         int    `json:"failures"`
	DialsInFlight    int64  `json:"dials_in_flight"`
	RetryAfterMS     int64  `json:"retry_after_ms,omitempty"`
	DialSuccessTotal uint64 `json:"dial_success_total"`
	DialFailureTotal uint64 `json:"dial_failure_total"`
	DialTimeoutTotal uint64 `json:"dial_timeout_total"`
	FastFailTotal    uint64 `json:"fast_fail_total"`
	CircuitOpenTotal uint64 `json:"circuit_open_total"`
	LastError        string `json:"last_error,omitempty"`
	LastTouched      string `json:"last_touched"`
}

type TargetCircuitBreaker struct {
	mu                  sync.Mutex
	states              map[string]*TargetCircuitState
	threshold           int
	failureWindow       time.Duration
	initialOpenDuration time.Duration
	maxOpenDuration     time.Duration
	stateTTL            time.Duration
	cleanupInterval     time.Duration
	maxStates           int
	lastCleanup         time.Time
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
		failureWindow:       defaultTargetCircuitFailureWindow,
		initialOpenDuration: initialOpenDuration,
		maxOpenDuration:     maxDuration,
		stateTTL:            defaultTargetCircuitStateTTL,
		cleanupInterval:     defaultTargetCircuitCleanupInterval,
		maxStates:           defaultTargetCircuitMaxStates,
		now:                 time.Now,
	}
}

func (b *TargetCircuitBreaker) BeforeDial(connType string, targetHost string) (*TargetDialPermit, error) {
	if b == nil {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.cleanupLocked(now, false)
	key := targetCircuitKey(connType, targetHost)
	state := b.states[key]
	if state == nil {
		b.ensureCapacityLocked(now)
		state = &TargetCircuitState{connType: connType, targetHost: targetHost, generation: 1, lastTouched: now}
		b.states[key] = state
	}
	state.lastTouched = now
	if state.probeInFlight {
		state.fastFailTotal++
		return nil, state.openError(targetHost, time.Millisecond)
	}
	if !state.openUntil.IsZero() {
		if state.openUntil.After(now) {
			state.fastFailTotal++
			return nil, state.openError(targetHost, state.openUntil.Sub(now))
		}
		state.openUntil = time.Time{}
		state.probeInFlight = true
		state.generation++
	}
	state.dialsInFlight++
	return &TargetDialPermit{key: key, generation: state.generation}, nil
}

func (b *TargetCircuitBreaker) AfterDial(permit *TargetDialPermit, err error) *TargetCircuitOpenError {
	if b == nil || permit == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	state := b.states[permit.key]
	if state == nil {
		return nil
	}
	if state.dialsInFlight > 0 {
		state.dialsInFlight--
	}
	state.lastTouched = now
	if err == nil {
		state.dialSuccessTotal++
	} else {
		state.dialFailureTotal++
		if isTargetDialTimeout(err) {
			state.dialTimeoutTotal++
		}
	}
	if permit.generation != state.generation {
		return nil
	}
	if err == nil {
		state.failures = 0
		state.failureWindowStart = time.Time{}
		state.openUntil = time.Time{}
		state.lastError = ""
		state.openDuration = 0
		state.probeInFlight = false
		state.generation++
		return nil
	}
	state.probeInFlight = false
	if state.openDuration <= 0 {
		if state.failures == 0 || state.failureWindowStart.IsZero() {
			state.failureWindowStart = now
		} else if b.failureWindow > 0 && now.Sub(state.failureWindowStart) > b.failureWindow {
			state.failures = 0
			state.failureWindowStart = now
		}
	}
	state.failures++
	state.lastError = err.Error()
	if state.failures >= b.threshold {
		duration := state.nextOpenDuration(b.initialOpenDuration, b.maxOpenDuration)
		state.openDuration = duration
		state.openUntil = now.Add(duration)
		state.circuitOpenTotal++
		state.generation++
		return state.openError(state.targetHost, duration)
	}
	return nil
}

func (b *TargetCircuitBreaker) AllOpen(connType string, targetHosts []string) (bool, error) {
	if b == nil || len(targetHosts) == 0 {
		return false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	var lastErr error
	for _, targetHost := range targetHosts {
		state := b.states[targetCircuitKey(connType, targetHost)]
		if state == nil {
			return false, nil
		}
		if state.probeInFlight {
			lastErr = state.openError(targetHost, time.Millisecond)
			continue
		}
		if state.openUntil.IsZero() || !state.openUntil.After(now) {
			return false, nil
		}
		lastErr = state.openError(targetHost, state.openUntil.Sub(now))
	}
	return true, lastErr
}

func (b *TargetCircuitBreaker) Metrics() []TargetCircuitMetric {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.cleanupLocked(now, true)
	metrics := make([]TargetCircuitMetric, 0, len(b.states))
	for _, state := range b.states {
		metric := TargetCircuitMetric{
			ConnType:         state.connType,
			Target:           state.targetHost,
			State:            state.metricState(now),
			Failures:         state.failures,
			DialsInFlight:    state.dialsInFlight,
			DialSuccessTotal: state.dialSuccessTotal,
			DialFailureTotal: state.dialFailureTotal,
			DialTimeoutTotal: state.dialTimeoutTotal,
			FastFailTotal:    state.fastFailTotal,
			CircuitOpenTotal: state.circuitOpenTotal,
			LastError:        state.lastError,
			LastTouched:      state.lastTouched.Format("2006-01-02 15:04:05.000"),
		}
		if state.openUntil.After(now) {
			metric.RetryAfterMS = state.openUntil.Sub(now).Milliseconds()
		}
		metrics = append(metrics, metric)
	}
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].ConnType != metrics[j].ConnType {
			return metrics[i].ConnType < metrics[j].ConnType
		}
		return metrics[i].Target < metrics[j].Target
	})
	return metrics
}

func (b *TargetCircuitBreaker) cleanupLocked(now time.Time, force bool) {
	if !force && !b.lastCleanup.IsZero() && now.Sub(b.lastCleanup) < b.cleanupInterval && (b.maxStates <= 0 || len(b.states) <= b.maxStates) {
		return
	}
	b.lastCleanup = now
	if b.stateTTL > 0 {
		for key, state := range b.states {
			if state.canEvict(now) && now.Sub(state.lastTouched) >= b.stateTTL {
				delete(b.states, key)
			}
		}
	}
	if b.maxStates > 0 && len(b.states) > b.maxStates {
		b.evictOldestIdleLocked(now, len(b.states)-b.maxStates)
	}
}

func (b *TargetCircuitBreaker) ensureCapacityLocked(now time.Time) {
	if b.maxStates <= 0 || len(b.states) < b.maxStates {
		return
	}
	b.evictOldestIdleLocked(now, len(b.states)-b.maxStates+1)
}

func (b *TargetCircuitBreaker) evictOldestIdleLocked(now time.Time, count int) {
	type candidate struct {
		key         string
		lastTouched time.Time
	}
	candidates := make([]candidate, 0, len(b.states))
	for key, state := range b.states {
		if state.canEvict(now) {
			candidates = append(candidates, candidate{key: key, lastTouched: state.lastTouched})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].lastTouched.Before(candidates[j].lastTouched) })
	if count > len(candidates) {
		count = len(candidates)
	}
	for i := 0; i < count; i++ {
		delete(b.states, candidates[i].key)
	}
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

func isTargetDialTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
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

func (s *TargetCircuitState) openError(targetHost string, retryAfter time.Duration) *TargetCircuitOpenError {
	return &TargetCircuitOpenError{Target: targetHost, RetryAfter: retryAfter, LastError: s.lastError}
}

func (s *TargetCircuitState) canEvict(now time.Time) bool {
	return s.dialsInFlight == 0 && !s.probeInFlight && !s.openUntil.After(now)
}

func (s *TargetCircuitState) metricState(now time.Time) string {
	if s.probeInFlight {
		return "half_open"
	}
	if s.openUntil.After(now) {
		return "open"
	}
	return "closed"
}
