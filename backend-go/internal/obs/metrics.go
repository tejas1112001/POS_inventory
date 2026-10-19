package obs

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the process metrics. Labels never carry tenant IDs, user IDs,
// slugs, raw paths or emails (OPERATIONS §3 cardinality rule).
type Metrics struct {
	Registry        *prometheus.Registry
	HTTPRequests    *prometheus.CounterVec
	HTTPDuration    *prometheus.HistogramVec
	RateLimited     *prometheus.CounterVec
	SessionTampered prometheus.Counter
	ListenerUp      prometheus.Gauge
	ListenerRetries prometheus.Counter
}

// NewMetrics registers the metrics on a fresh registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pos_http_requests_total",
			Help: "HTTP requests by route pattern, method, status class and host kind.",
		}, []string{"route", "method", "status_class", "host_kind"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pos_http_request_duration_seconds",
			Help:    "HTTP request duration by route pattern and method.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}, []string{"route", "method"}),
		RateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pos_rate_limited_total",
			Help: "Requests rejected by a rate limiter.",
		}, []string{"limiter"}),
		SessionTampered: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pos_session_tampered_total",
			Help: "Tenant transactions whose role or search_path changed unexpectedly.",
		}),
		ListenerUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pos_listener_connected",
			Help: "1 while the LISTEN connection is up and subscribed.",
		}),
		ListenerRetries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pos_listener_reconnects_total",
			Help: "LISTEN connection reconnects.",
		}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.RateLimited, m.SessionTampered,
		m.ListenerUp, m.ListenerRetries,
	)
	return m
}

// Handler serves /metrics. Mount it only on the internal listener.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
}
