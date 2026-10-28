package operator

import (
	"strconv"
	"sync"
	"time"

	"github.com/garamsh/garam/platform/internal/errorx"
)

// Exceeded refuses a call past the bound, carrying how long until the
// window that refused it closes, which an answer's Retry-After states.
type Exceeded struct {
	RetryAfter time.Duration
}

func (e *Exceeded) Error() string {
	return "rate limit exceeded: retry after " + strconv.Itoa(e.RetryAfterSeconds()) + "s"
}

// Kind places the refusal under resource_exhausted, which both HTTP
// surfaces answer 429.
func (e *Exceeded) Kind() errorx.Kind { return errorx.KindResourceExhausted }

// Unwrap answers nil: the refusal wraps nothing.
func (e *Exceeded) Unwrap() error { return nil }

// RetryAfterSeconds rounds RetryAfter up to whole seconds, at least
// one.
func (e *Exceeded) RetryAfterSeconds() int {
	seconds := int((e.RetryAfter + time.Second - 1) / time.Second)
	return max(seconds, 1)
}

// Limiter bounds how often one key — a client_id, an operator — may
// exchange or be issued a key inside one process (ADR-0102): a fixed
// window per key over a bounded set of keys, so neither the count nor
// the memory grows with what callers send. It holds no shared state,
// so its bound is per replica.
type Limiter struct {
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time

	mu      sync.Mutex
	windows map[string]*fixedWindow
}

type fixedWindow struct {
	start time.Time
	count int
}

// LimiterConfig is a limiter's bound.
type LimiterConfig struct {
	// Limit is the calls admitted per key per Window.
	Limit int
	// Window is the length of one counting window.
	Window time.Duration
	// MaxKeys bounds how many keys are tracked at once. A new key
	// arriving while that many hold an open window is refused until
	// one closes, so a caller spraying keys saturates the limiter
	// rather than its memory.
	MaxKeys int
	// Now supplies the time; zero takes time.Now.
	Now func() time.Time
}

// NewLimiter constructs a limiter. A Limit, Window or MaxKeys below
// one is a programming error and panics.
func NewLimiter(cfg LimiterConfig) *Limiter {
	if cfg.Limit < 1 || cfg.Window <= 0 || cfg.MaxKeys < 1 {
		panic("operator: a limiter's Limit, Window and MaxKeys must be positive")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Limiter{limit: cfg.Limit, window: cfg.Window, maxKeys: cfg.MaxKeys, now: now,
		windows: map[string]*fixedWindow{}}
}

// Check counts a call for key and answers nil where it is admitted,
// and an *Exceeded where it is not.
func (l *Limiter) Check(key string) error {
	ok, retryAfter := l.allow(key)
	if ok {
		return nil
	}
	return &Exceeded{RetryAfter: retryAfter}
}

func (l *Limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, ok := l.windows[key]
	if ok && now.Sub(w.start) >= l.window {
		delete(l.windows, key)
		ok = false
	}
	if !ok {
		if len(l.windows) >= l.maxKeys {
			l.sweep(now)
		}
		if len(l.windows) >= l.maxKeys {
			return false, l.window
		}
		w = &fixedWindow{start: now}
		l.windows[key] = w
	}
	if w.count >= l.limit {
		return false, w.start.Add(l.window).Sub(now)
	}
	w.count++
	return true, 0
}

// sweep drops every window that has closed.
func (l *Limiter) sweep(now time.Time) {
	for key, w := range l.windows {
		if now.Sub(w.start) >= l.window {
			delete(l.windows, key)
		}
	}
}
