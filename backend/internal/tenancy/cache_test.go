package tenancy

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestCache(max int) (*TTLCache[string, int], *clock) {
	c := NewTTLCache[string, int](max)
	clk := &clock{t: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	c.now = clk.now
	return c, clk
}

func TestTTLCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c, _ := newTestCache(3)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Set("c", 3, time.Minute)
	_, ok := c.Get("a") // a is now the most recently used
	require.True(t, ok)
	c.Set("d", 4, time.Minute) // evicts b
	_, ok = c.Get("b")
	require.False(t, ok)
	for _, k := range []string{"a", "c", "d"} {
		_, ok := c.Get(k)
		require.True(t, ok, k)
	}
	require.Equal(t, 3, c.Len())
	require.Equal(t, uint64(1), c.Evicted())
}

func TestTTLCacheNeverGrowsPastMax(t *testing.T) {
	c, _ := newTestCache(100)
	for i := range 10_000 {
		c.Set("slug-"+strconv.Itoa(i), i, time.Hour)
	}
	require.Equal(t, 100, c.Len())
	require.Equal(t, uint64(9_900), c.Evicted())
	v, ok := c.Get("slug-9999")
	require.True(t, ok)
	require.Equal(t, 9999, v)
}

func TestTTLCacheUpdateKeepsOneEntry(t *testing.T) {
	c, _ := newTestCache(2)
	c.Set("a", 1, time.Minute)
	c.Set("a", 2, time.Minute)
	c.Set("b", 3, time.Minute)
	v, ok := c.Get("a")
	require.True(t, ok)
	require.Equal(t, 2, v)
	require.Equal(t, 2, c.Len())
	require.Zero(t, c.Evicted())
}

func TestTTLCacheExpiryAndSweep(t *testing.T) {
	c, clk := newTestCache(10)
	c.Set("short", 1, 10*time.Second)
	c.Set("long", 2, time.Hour)
	clk.t = clk.t.Add(11 * time.Second)
	_, ok := c.Get("short")
	require.False(t, ok)
	require.Equal(t, 1, c.Len(), "an expired entry is dropped when read")

	// Entries nobody reads again are swept by a later Set, at most once a
	// minute.
	for i := range 5 {
		c.Set("unread-"+strconv.Itoa(i), i, 10*time.Second)
	}
	require.Equal(t, 6, c.Len())
	clk.t = clk.t.Add(2 * time.Minute)
	c.Set("new", 9, time.Minute)
	require.Equal(t, 2, c.Len(), "only long and new are left")
	_, ok = c.Get("long")
	require.True(t, ok)
}

func TestTTLCacheDeleteFuncAndFlush(t *testing.T) {
	c, _ := newTestCache(10)
	for i := range 6 {
		c.Set(strconv.Itoa(i), i, time.Minute)
	}
	c.DeleteFunc(func(_ string, v int) bool { return v%2 == 0 })
	require.Equal(t, 3, c.Len())
	_, ok := c.Get("2")
	require.False(t, ok)
	c.Delete("1")
	require.Equal(t, 2, c.Len())
	c.Flush()
	require.Zero(t, c.Len())
	c.Set("x", 1, time.Minute) // still usable after a flush
	_, ok = c.Get("x")
	require.True(t, ok)
}

func TestTTLCacheMinimumSize(t *testing.T) {
	c := NewTTLCache[string, int](0)
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	require.Equal(t, 1, c.Len())
}
