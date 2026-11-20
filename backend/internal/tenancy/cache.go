package tenancy

import (
	"container/list"
	"sync"
	"time"
)

// TTLCache is a mutex-guarded cache with per-entry expiry and a fixed
// maximum size (ARCHITECTURE_REVIEW H4). When it is full, the least
// recently used entry makes room, so keys an attacker controls (random
// subdomains, date ranges in a query) can never grow memory without
// bound. Expired entries are dropped when read and, at most once a
// minute, by a sweep run from Set; no goroutine is started. Caches here
// hold tenants, auth state and permissions and are invalidated by NOTIFY.
type TTLCache[K comparable, V any] struct {
	mu        sync.Mutex
	max       int
	items     map[K]*list.Element
	order     *list.List // front = most recently used
	now       func() time.Time
	lastSweep time.Time
	evicted   uint64
}

type ttlItem[K comparable, V any] struct {
	k   K
	v   V
	exp time.Time
}

// sweepEvery is how often Set walks the cache for expired entries.
const sweepEvery = time.Minute

// NewTTLCache creates an empty cache holding at most max entries (at
// least 1).
func NewTTLCache[K comparable, V any](max int) *TTLCache[K, V] {
	if max < 1 {
		max = 1
	}
	return &TTLCache[K, V]{max: max, items: map[K]*list.Element{}, order: list.New(), now: time.Now}
}

// Get returns a live entry and marks it recently used.
func (c *TTLCache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	e, ok := c.items[k]
	if !ok {
		return zero, false
	}
	it := e.Value.(*ttlItem[K, V])
	if !c.now().Before(it.exp) {
		c.remove(e)
		return zero, false
	}
	c.order.MoveToFront(e)
	return it.v, true
}

// Set stores v for ttl. When the cache is full, the least recently used
// entry is evicted.
func (c *TTLCache[K, V]) Set(k K, v V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.lastSweep) >= sweepEvery {
		c.sweep(now)
	}
	if e, ok := c.items[k]; ok {
		it := e.Value.(*ttlItem[K, V])
		it.v, it.exp = v, now.Add(ttl)
		c.order.MoveToFront(e)
		return
	}
	c.items[k] = c.order.PushFront(&ttlItem[K, V]{k: k, v: v, exp: now.Add(ttl)})
	for len(c.items) > c.max {
		c.remove(c.order.Back())
		c.evicted++
	}
}

// Delete removes one entry.
func (c *TTLCache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[k]; ok {
		c.remove(e)
	}
}

// DeleteFunc removes entries for which drop returns true.
func (c *TTLCache[K, V]) DeleteFunc(drop func(K, V) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for e := c.order.Front(); e != nil; {
		next := e.Next()
		if it := e.Value.(*ttlItem[K, V]); drop(it.k, it.v) {
			c.remove(e)
		}
		e = next
	}
}

// Flush removes everything.
func (c *TTLCache[K, V]) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
	c.order.Init()
}

// Len counts entries, including expired ones not yet swept.
func (c *TTLCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Evicted counts live entries dropped to make room since the cache was
// created (a sign the size limit is too small, or of a flood of keys).
func (c *TTLCache[K, V]) Evicted() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evicted
}

func (c *TTLCache[K, V]) remove(e *list.Element) {
	delete(c.items, e.Value.(*ttlItem[K, V]).k)
	c.order.Remove(e)
}

// sweep drops every expired entry. Callers hold mu.
func (c *TTLCache[K, V]) sweep(now time.Time) {
	c.lastSweep = now
	for e := c.order.Front(); e != nil; {
		next := e.Next()
		if !now.Before(e.Value.(*ttlItem[K, V]).exp) {
			c.remove(e)
		}
		e = next
	}
}
