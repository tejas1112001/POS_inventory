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

	// Mail and receipts (EMAIL_AND_RECEIPTS §6). No tenant labels.
	EmailSent      *prometheus.CounterVec
	EmailFailed    *prometheus.CounterVec
	EmailDead      prometheus.Counter
	OutboxDepth    *prometheus.GaugeVec
	ReceiptRender  prometheus.Histogram
	LedgerMismatch prometheus.Counter
	JobsFailed     *prometheus.CounterVec

	// Billing (BILLING_DESIGN §4). No tenant labels.
	WebhookSignatureFailures prometheus.Counter
	BillingEvents            *prometheus.CounterVec
	// Reconcile (S10 item 3): open differences with the provider, and the
	// age of the newest verified webhook while live subscriptions exist
	// (0 when there are none).
	BillingDriftOpen   prometheus.Gauge
	BillingWebhookAge  prometheus.Gauge
	BillingFetchErrors prometheus.Counter
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
		EmailSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pos_email_sent_total",
			Help: "Emails accepted by an SMTP server.",
		}, []string{"via", "kind"}),
		EmailFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pos_email_failed_total",
			Help: "Email send attempts that failed, by error class.",
		}, []string{"class"}),
		EmailDead: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pos_email_dead_total",
			Help: "Emails given up on (dead letter).",
		}),
		OutboxDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pos_email_outbox_depth",
			Help: "Emails waiting to be sent, summed over tenants.",
		}, []string{"scope"}),
		ReceiptRender: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "pos_receipt_render_seconds",
			Help:    "Time to render and store one receipt PDF.",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}),
		LedgerMismatch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pos_ledger_mismatch_total",
			Help: "Inventory rows whose quantity differs from the stock ledger (nightly check).",
		}),
		JobsFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pos_jobs_failed_total",
			Help: "Background job runs that returned an error.",
		}, []string{"kind"}),
		WebhookSignatureFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pos_webhook_signature_failures_total",
			Help: "Billing webhooks refused for a missing or invalid signature (alert).",
		}),
		BillingEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pos_billing_events_total",
			Help: "Verified billing webhook events by outcome (applied, duplicate, stale, ...).",
		}, []string{"outcome"}),
		BillingDriftOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pos_billing_drift_open",
			Help: "Open differences between our subscriptions and the provider's (hourly reconcile).",
		}),
		BillingWebhookAge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pos_billing_webhook_age_seconds",
			Help: "Seconds since the last verified billing webhook while live subscriptions exist (0: none live).",
		}),
		BillingFetchErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pos_billing_reconcile_fetch_errors_total",
			Help: "Provider fetches that failed during the reconcile (network or 5xx).",
		}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.RateLimited, m.SessionTampered,
		m.ListenerUp, m.ListenerRetries,
		m.EmailSent, m.EmailFailed, m.EmailDead, m.OutboxDepth, m.ReceiptRender, m.LedgerMismatch, m.JobsFailed,
		m.WebhookSignatureFailures, m.BillingEvents, m.BillingDriftOpen, m.BillingWebhookAge, m.BillingFetchErrors,
	)
	return m
}

// Handler serves /metrics. Mount it only on the internal listener.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
}
