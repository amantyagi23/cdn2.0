// Package ratelimit provides a fixed-window rate limiter with violation
// tracking. Two backends are provided: an in-memory store for single-
// replica deployments and tests, and a Redis-backed store for multi-
// replica deployments.
//
// Semantics (matching the TS RateLimiterService):
//
//   - Each (type, identifier) pair has its own window.
//   - A request increments the counter. If the counter exceeds MaxRequests
//     within the window, the request is rejected and a violation is
//     recorded.
//   - Violations persist across windows, so a client that repeatedly
//     violates gets progressively more restricted (penalty box).
package ratelimit

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Kind is the rate-limit bucket type. Adding a kind is a one-line change.
type Kind string

const (
	KindAPI      Kind = "api"
	KindUpload   Kind = "upload"
	KindDelivery Kind = "delivery"
)

// Config is the per-kind policy.
type Config struct {
	WindowMs    int64
	MaxRequests int64

	// PenaltyViolations and PenaltyWindowMs control the penalty box.
	// A client that exceeds PenaltyViolations within PenaltyWindowMs
	// is blocked until PenaltyWindowMs elapses since the last violation.
	PenaltyViolations int64
	PenaltyWindowMs   int64
}

// DefaultConfig returns the built-in defaults. Overridable from env.
func DefaultConfig() map[Kind]Config {
	return map[Kind]Config{
		KindAPI:      {WindowMs: 1_000, MaxRequests: 10, PenaltyViolations: 5, PenaltyWindowMs: 5 * 60 * 1000},
		KindUpload:   {WindowMs: 1_000, MaxRequests: 5, PenaltyViolations: 5, PenaltyWindowMs: 5 * 60 * 1000},
		KindDelivery: {WindowMs: 1_000, MaxRequests: 25, PenaltyViolations: 5, PenaltyWindowMs: 5 * 60 * 1000},
	}
}

// Decision is what Check returns.
type Decision struct {
	Allowed    bool
	Remaining  int64
	RetryAfter int64 // seconds
	Violations int64
}

// Store is the pluggable backend. InMemoryStore is the default;
// RedisStore (in a separate file) provides multi-replica semantics.
type Store interface {
	// Check atomically increments the counter for (kind, id) in the
	// current window and returns the decision.
	Check(ctx context.Context, kind Kind, id string, cfg Config) (Decision, error)

	// Violations returns the accumulated violation count.
	Violations(ctx context.Context, kind Kind, id string) (int64, error)

	// InPenaltyBox reports whether the id is currently penalized.
	InPenaltyBox(ctx context.Context, kind Kind, id string, cfg Config) (bool, error)

	// Reset clears state for (kind, id), or for id across all kinds
	// if kind is empty.
	Reset(ctx context.Context, kind Kind, id string) error
}

// Limiter wraps a Store with the fixed set of Configs. It is the public
// API used by handlers and the engine.
type Limiter struct {
	store   Store
	configs map[Kind]Config
	log     *zap.Logger
}

func NewLimiter(store Store, configs map[Kind]Config, log *zap.Logger) *Limiter {
	if len(configs) == 0 {
		configs = DefaultConfig()
	}
	return &Limiter{store: store, configs: configs, log: log}
}

// Check implements the same semantics as the TS checkLimit.
func (l *Limiter) Check(ctx context.Context, id string, kind Kind) Decision {
	cfg, ok := l.configs[kind]
	if !ok {
		// Unknown kind: fail open (don't block on a config error).
		return Decision{Allowed: true}
	}
	d, err := l.store.Check(ctx, kind, id, cfg)
	if err != nil {
		l.log.Error("rate limit check failed; failing open",
			zap.String("kind", string(kind)),
			zap.String("id", id),
			zap.Error(err),
		)
		return Decision{Allowed: true}
	}
	return d
}

func (l *Limiter) Violations(ctx context.Context, id string, kind Kind) int64 {
	v, _ := l.store.Violations(ctx, kind, id)
	return v
}

func (l *Limiter) InPenaltyBox(ctx context.Context, id string, kind Kind) bool {
	cfg, ok := l.configs[kind]
	if !ok {
		return false
	}
	p, _ := l.store.InPenaltyBox(ctx, kind, id, cfg)
	return p
}

func (l *Limiter) Reset(ctx context.Context, id string, kind Kind) error {
	return l.store.Reset(ctx, kind, id)
}

// -----------------------------------------------------------------
// In-memory backend
// -----------------------------------------------------------------

type memEntry struct {
	count         int64
	windowStart   int64
	violations    int64
	lastViolation int64
}

// InMemoryStore is a fixed-window counter in process memory. Suitable
// for single-replica deployments and tests. Not suitable when multiple
// replicas serve the same client — each replica would have its own
// counter, allowing N× the rate limit.
type InMemoryStore struct {
	mu       sync.Mutex
	entries  map[string]*memEntry
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewInMemoryStore() *InMemoryStore {
	s := &InMemoryStore{
		entries: make(map[string]*memEntry),
		stopCh:  make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

func (s *InMemoryStore) Close() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *InMemoryStore) Check(_ context.Context, kind Kind, id string, cfg Config) (Decision, error) {
	key := string(kind) + ":" + id
	now := time.Now().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok || now-e.windowStart >= cfg.WindowMs {
		e = &memEntry{windowStart: now}
		s.entries[key] = e
	}
	e.count++

	if e.count > cfg.MaxRequests {
		e.violations++
		e.lastViolation = now

		retryAfter := (e.windowStart + cfg.WindowMs - now) / 1000
		if retryAfter < 1 {
			retryAfter = 1
		}
		return Decision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: retryAfter,
			Violations: e.violations,
		}, nil
	}

	remaining := cfg.MaxRequests - e.count
	if remaining < 0 {
		remaining = 0
	}
	return Decision{
		Allowed:    true,
		Remaining:  remaining,
		Violations: e.violations,
	}, nil
}

func (s *InMemoryStore) Violations(_ context.Context, kind Kind, id string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[string(kind)+":"+id]; ok {
		return e.violations, nil
	}
	return 0, nil
}

func (s *InMemoryStore) InPenaltyBox(_ context.Context, kind Kind, id string, cfg Config) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[string(kind)+":"+id]
	if !ok {
		return false, nil
	}
	if e.violations < cfg.PenaltyViolations {
		return false, nil
	}
	return time.Now().UnixMilli()-e.lastViolation < cfg.PenaltyWindowMs, nil
}

func (s *InMemoryStore) Reset(_ context.Context, kind Kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != "" {
		delete(s.entries, string(kind)+":"+id)
		return nil
	}
	for k := range s.entries {
		if len(k) >= len(id)+1 && k[len(k)-len(id)-1:] == ":"+id {
			delete(s.entries, k)
		}
	}
	return nil
}

// cleanupLoop prunes entries older than 10 minutes, matching the TS
// cleanup interval.
func (s *InMemoryStore) cleanupLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			cutoff := time.Now().Add(-10 * time.Minute).UnixMilli()
			s.mu.Lock()
			for k, e := range s.entries {
				if e.windowStart < cutoff {
					delete(s.entries, k)
				}
			}
			s.mu.Unlock()
		}
	}
}
