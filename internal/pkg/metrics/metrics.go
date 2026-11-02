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

	// PaymentStatusChanges counts payments that changed status, by the new status and who decided it:
	// "gateway" (webhook, reconciliation, refund confirmation), "worker" (expiry, refused checkout)
	// or "manual" (attendant).
	PaymentStatusChanges = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "payment_status_changes_total",
			Help: "Payments that changed status, by new status and source.",
		},
		[]string{"to", "source"},
	)

	// GatewayRequests counts calls to the payment gateway by operation and outcome
	// (ok, transient or permanent), GatewayRequestDuration times them.
	GatewayRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Calls to the payment gateway, by operation and outcome (ok, transient, permanent).",
		},
		[]string{"op", "outcome"},
	)
	GatewayRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_request_duration_seconds",
			Help:    "Payment gateway call latency in seconds, by operation.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"op"},
	)

	// GatewayWorkerErrors counts failed steps of the gateway worker (a step that is only waiting for the
	// gateway is not an error).
	GatewayWorkerErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_worker_errors_total",
			Help: "Failed steps of the gateway worker, by step and kind (transient, permanent).",
		},
		[]string{"step", "kind"},
	)

	// PaymentsPending and PaymentsPendingOldestAge describe the Mercado Pago payments waiting for the
	// customer or the gateway; the worker refreshes them on every pass.
	PaymentsPending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "payments_pending",
		Help: "Mercado Pago payments in PENDING.",
	})
	PaymentsPendingOldestAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "payments_pending_oldest_age_seconds",
		Help: "Age of the oldest Mercado Pago payment in PENDING (0 when there is none).",
	})

	// PaymentsUnsettled counts the times the worker or the webhook saw money that Billing did not
	// settle and that needs a person: reason is extra_approval, late_approval or amount_mismatch.
	// Alert on any increase.
	PaymentsUnsettled = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "payments_unsettled_total",
			Help: "Approved gateway payments that were not settled and need a manual refund or review, by reason.",
		},
		[]string{"reason"},
	)
)

func init() {
	prometheus.MustRegister(HTTPRequestDuration, OutboxPending, PaymentStatusChanges, GatewayRequests, GatewayRequestDuration,
		GatewayWorkerErrors, PaymentsPending, PaymentsPendingOldestAge, PaymentsUnsettled)
}
