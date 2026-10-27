package httpx

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Check is one readiness check. It must honour ctx.
type Check func(ctx context.Context) error

// Health serves /healthz and /readyz (OPERATIONS §1).
type Health struct {
	draining atomic.Bool
	mu       sync.RWMutex
	checks   map[string]Check
	timeout  time.Duration
}

// NewHealth creates a health handler with a per-check timeout.
func NewHealth(timeout time.Duration) *Health {
	return &Health{checks: map[string]Check{}, timeout: timeout}
}

// Add registers a named readiness check.
func (h *Health) Add(name string, c Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = c
}

// SetDraining makes /readyz fail so the proxy stops sending traffic.
func (h *Health) SetDraining() { h.draining.Store(true) }

// Draining reports whether shutdown has started.
func (h *Health) Draining() bool { return h.draining.Load() }

// Live answers if the process can serve HTTP at all. No dependencies, no
// version details.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready runs every check in parallel. On failure it lists check names only,
// never error text.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready", "checks": map[string]string{"draining": "fail"},
		})
		return
	}
	h.mu.RLock()
	checks := make(map[string]Check, len(h.checks))
	for k, v := range h.checks {
		checks[k] = v
	}
	h.mu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed = map[string]string{}
	)
	for name, c := range checks {
		wg.Go(func() {
			if err := c(ctx); err != nil {
				mu.Lock()
				failed[name] = "fail"
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(failed) > 0 {
		WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "checks": failed})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
