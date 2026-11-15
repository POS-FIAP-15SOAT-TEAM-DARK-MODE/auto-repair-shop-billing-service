//go:build sandbox

// Smoke tests of the Gateway against the real Mercado Pago sandbox. They are not part of `make test`
// or the CI: they need the network and a seller test credential, and each run leaves a few
// preferences in the test account. Run them with `make test-sandbox` (reads .env) before a release
// and whenever Mercado Pago changes its API. Env: MERCADOPAGO_ACCESS_TOKEN.
package mercadopago

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
)

func sandboxGateway(t *testing.T) *Gateway {
	t.Helper()
	token := os.Getenv("MERCADOPAGO_ACCESS_TOKEN")
	if token == "" {
		t.Skip("MERCADOPAGO_ACCESS_TOKEN is not set")
	}
	g, err := New(token)
	require.NoError(t, err)
	return g
}

func sandboxCheckout(t *testing.T, g *Gateway) (payment.CheckoutRequest, payment.Checkout) {
	t.Helper()
	req := payment.CheckoutRequest{
		PaymentID: uuid.NewString(), // the external_reference: unique per run, nothing else uses it
		Title:     "Billing sandbox smoke test",
		Amount:    decimal.RequireFromString("10.00"),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	co, err := g.CreateCheckout(context.Background(), req)
	require.NoError(t, err)
	return req, co
}

func TestSandbox_CreateCheckoutReturnsAnInitPoint(t *testing.T) {
	g := sandboxGateway(t)
	_, co := sandboxCheckout(t, g)

	assert.NotEmpty(t, co.ExternalID)
	assert.True(t, strings.HasPrefix(co.URL, "https://www.mercadopago."), "init_point, not sandbox_init_point: %s", co.URL)
	assert.Contains(t, co.URL, co.ExternalID)
	t.Cleanup(func() { _ = g.ExpireCheckout(context.Background(), co.ExternalID) })
}

func TestSandbox_CreateCheckoutIsIdempotentPerPayment(t *testing.T) {
	g := sandboxGateway(t)
	req, first := sandboxCheckout(t, g)
	t.Cleanup(func() { _ = g.ExpireCheckout(context.Background(), first.ExternalID) })

	time.Sleep(5 * time.Second) // the search sees a new preference after about 3 s; the worker retries after its 5 s backoff
	again, err := g.CreateCheckout(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, first.ExternalID, again.ExternalID, "a retry must not open a second checkout")
}

func TestSandbox_FindPaymentsOfAFreshCheckoutIsEmpty(t *testing.T) {
	g := sandboxGateway(t)
	req, co := sandboxCheckout(t, g)
	t.Cleanup(func() { _ = g.ExpireCheckout(context.Background(), co.ExternalID) })

	found, err := g.FindPayments(context.Background(), req.PaymentID)

	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestSandbox_ExpireCheckoutAcceptsTheCheckoutAndRepeats(t *testing.T) {
	g := sandboxGateway(t)
	_, co := sandboxCheckout(t, g)

	require.NoError(t, g.ExpireCheckout(context.Background(), co.ExternalID))
	assert.NoError(t, g.ExpireCheckout(context.Background(), co.ExternalID), "closing twice is harmless")
}

func TestSandbox_GetPaymentOfAnUnknownIdIsNotFound(t *testing.T) {
	g := sandboxGateway(t)

	_, err := g.GetPayment(context.Background(), "1")

	require.Error(t, err)
	assert.True(t, payment.IsNotFound(err), "a 404 is what makes a notification of an unknown payment acknowledged: %v", err)
	assert.False(t, payment.IsTransient(err))
}

func TestSandbox_ARefusedTokenIsRetriedNotFatal(t *testing.T) {
	bad, err := New(fmt.Sprintf("APP_USR-%s", uuid.NewString()))
	require.NoError(t, err)

	_, err = bad.CreateCheckout(context.Background(), payment.CheckoutRequest{
		PaymentID: uuid.NewString(), Title: "x", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now().Add(time.Hour)})

	require.Error(t, err)
	assert.True(t, payment.IsTransient(err), "a wrong token must be retried and logged, not fail the customer's payment: %v", err)
}
