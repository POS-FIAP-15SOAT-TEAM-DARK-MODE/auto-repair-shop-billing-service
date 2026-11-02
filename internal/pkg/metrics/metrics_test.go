package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsAreRegisteredWithLintCleanNames(t *testing.T) {
	// touch one series of each vector so the registry has something to lint
	PaymentStatusChanges.WithLabelValues("APPROVED", "gateway")
	GatewayRequests.WithLabelValues("get_payment", "ok")
	GatewayRequestDuration.WithLabelValues("get_payment")
	GatewayWorkerErrors.WithLabelValues("REFUND", "transient")
	PaymentsUnsettled.WithLabelValues("amount_mismatch")
	GatewayWorkerPasses.WithLabelValues("ran")

	problems, err := testutil.GatherAndLint(prometheus.DefaultGatherer,
		"payment_status_changes_total", "gateway_requests_total", "gateway_request_duration_seconds",
		"gateway_worker_errors_total", "gateway_worker_passes_total", "refunds_pending", "refunds_pending_oldest_age_seconds", "payments_pending", "payments_pending_oldest_age_seconds", "payments_unsettled_total")
	require.NoError(t, err)
	assert.Empty(t, problems)
}
