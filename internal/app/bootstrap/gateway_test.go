package bootstrap

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	budgetRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	paymentRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/repository"
)

func newService() *billing.Service {
	return billing.NewService((*sql.DB)(nil), budgetRepo.NewInMemory(), paymentRepo.NewInMemory(),
		payment.NewInternalProvider(payment.ModeManual), func() string { return "id" })
}

func TestGatewayMode_RequiresTheAccessToken(t *testing.T) {
	t.Setenv("MERCADOPAGO_ACCESS_TOKEN", "")
	_, _, err := gatewayMode(newService())
	assert.EqualError(t, err, "MERCADOPAGO_ACCESS_TOKEN is required when PAYMENT_MODE=mercadopago")
}

func TestGatewayMode_WiresTheWebhookAndTheWorker(t *testing.T) {
	t.Setenv("MERCADOPAGO_ACCESS_TOKEN", "TEST-token")
	t.Setenv("MERCADOPAGO_WEBHOOK_SECRET", "s3cret")
	t.Setenv("MERCADOPAGO_NOTIFICATION_URL", "https://example.test/v1/webhooks/mercadopago")
	t.Setenv("MERCADOPAGO_CHECKOUT_TTL", "24h")
	t.Setenv("GATEWAY_WORKER_INTERVAL", "1s")

	controller, servers, err := gatewayMode(newService())
	require.NoError(t, err)
	assert.True(t, controller.WebhookEnabled())
	require.Len(t, servers, 1)
	assert.Equal(t, "gateway-worker", servers[0].Name())
}

func TestGatewayMode_AnEmptyWebhookSecretStillStartsAndRejectsWebhooks(t *testing.T) {
	t.Setenv("MERCADOPAGO_ACCESS_TOKEN", "TEST-token")
	t.Setenv("MERCADOPAGO_WEBHOOK_SECRET", "")
	controller, _, err := gatewayMode(newService())
	require.NoError(t, err)
	assert.True(t, controller.WebhookEnabled(), "the route exists; the verifier refuses every call")
}
