package app

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// registryCacheCollector reports the tenant registry cache (ARCHITECTURE_REVIEW
// H4): entries by kind and how many were evicted to stay under the size
// limit. A rising eviction count means a flood of unknown subdomains or a
// limit that is too small. No tenant labels.
func registryCacheCollector(reg *tenancy.Registry) prometheus.Collector {
	return &cacheCollector{
		reg: reg,
		entries: prometheus.NewDesc("pos_registry_cache_entries", "Tenant registry cache entries, by kind (found or miss).",
			[]string{"kind"}, nil),
		evicted: prometheus.NewDesc("pos_registry_cache_evictions_total", "Registry cache entries evicted to stay under the size limit.",
			nil, nil),
	}
}

type cacheCollector struct {
	reg              *tenancy.Registry
	entries, evicted *prometheus.Desc
}

func (c *cacheCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.entries
	ch <- c.evicted
}

func (c *cacheCollector) Collect(ch chan<- prometheus.Metric) {
	found, misses, evicted := c.reg.CacheStats()
	ch <- prometheus.MustNewConstMetric(c.entries, prometheus.GaugeValue, float64(found), "found")
	ch <- prometheus.MustNewConstMetric(c.entries, prometheus.GaugeValue, float64(misses), "miss")
	ch <- prometheus.MustNewConstMetric(c.evicted, prometheus.CounterValue, float64(evicted))
}
