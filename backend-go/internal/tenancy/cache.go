package tenancy

import (
	"sync"
	"time"
)

// TTLCache is a small mutex-guarded map with per-entry expiry. Caches here
// are tiny (tenants, auth state, permissions) and invalidated by NOTIFY.
type TTLCache[K comparable, V any] struct {
	mu    sync.Mutex
	items map[K]ttlItem[V]
	now   func() time.Time
}

type ttlItem[V any] struct {
	v   V
	exp time.Time
}

// NewTTLCache creates an empty cache.
func NewTTLCache[K comparable, V any]() *TTLCache[K, V] {
	return &TTLCache[K, V]{items: map[K]ttlItem[V]{}, now: time.Now}
}

// Get returns a live entry.
func (c *TTLCache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[k]
	if !ok || !c.now().Before(it.exp) {
		if ok {
			delete(c.items, k)
		}
		var zero V
		return zero, false
	}
	return it.v, true
}

// Set stores v for ttl.
func (c *TTLCache[K, V]) Set(k K, v V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[k] = ttlItem[V]{v: v, exp: c.now().Add(ttl)}
}

// Delete removes one entry.
func (c *TTLCache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, k)
}

// DeleteFunc removes entries for which drop returns true.
func (c *TTLCache[K, V]) DeleteFunc(drop func(K, V) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, it := range c.items {
		if drop(k, it.v) {
			delete(c.items, k)
		}
	}
}

// Flush removes everything.
func (c *TTLCache[K, V]) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
}

// Len counts entries, including expired ones not yet evicted.
func (c *TTLCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
