package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, by method, route and status.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)

	// OutboxPending counts outbox rows not yet published to SNS. It keeps growing
	// while the relay cannot reach SNS, which makes it the first signal of a stuck saga step.
	OutboxPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "outbox_pending",
			Help: "Outbox rows waiting to be published to the events topic.",
		},
	)
)

func init() {
	prometheus.MustRegister(HTTPRequestDuration, OutboxPending)
}
