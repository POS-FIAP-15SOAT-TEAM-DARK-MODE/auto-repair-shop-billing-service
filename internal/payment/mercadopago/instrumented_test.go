package mercadopago

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
)

type stubGateway struct{ err error }

func (s stubGateway) CreateCheckout(context.Context, payment.CheckoutRequest) (payment.Checkout, error) {
	return payment.Checkout{ExternalID: "pref"}, s.err
}
func (s stubGateway) FindPayments(context.Context, string) ([]payment.GatewayPayment, error) {
	return []payment.GatewayPayment{{ID: "1"}}, s.err
}
func (s stubGateway) GetPayment(context.Context, string) (payment.GatewayPayment, error) {
	return payment.GatewayPayment{ID: "1"}, s.err
}
func (s stubGateway) FindCheckout(context.Context, string) (payment.Checkout, bool, error) {
	return payment.Checkout{ExternalID: "pref"}, true, s.err
}
func (s stubGateway) Refund(context.Context, string) error         { return s.err }
func (s stubGateway) CancelPayment(context.Context, string) error  { return s.err }
func (s stubGateway) ExpireCheckout(context.Context, string) error { return s.err }

func count(op, outcome string) float64 {
	return testutil.ToFloat64(metrics.GatewayRequests.WithLabelValues(op, outcome))
}

func TestInstrument_CountsEveryOperationByOutcomeAndKeepsTheResults(t *testing.T) {
	ctx := context.Background()
	call := map[string]func(payment.Gateway) error{
		"create_checkout": func(g payment.Gateway) error { _, err := g.CreateCheckout(ctx, payment.CheckoutRequest{}); return err },
		"find_payments":   func(g payment.Gateway) error { _, err := g.FindPayments(ctx, "x"); return err },
		"find_checkout":   func(g payment.Gateway) error { _, _, err := g.FindCheckout(ctx, "x"); return err },
		"get_payment":     func(g payment.Gateway) error { _, err := g.GetPayment(ctx, "1"); return err },
		"refund":          func(g payment.Gateway) error { return g.Refund(ctx, "1") },
		"cancel_payment":  func(g payment.Gateway) error { return g.CancelPayment(ctx, "1") },
		"expire_checkout": func(g payment.Gateway) error { return g.ExpireCheckout(ctx, "pref") },
	}
	outcomes := map[string]error{
		"ok":        nil,
		"transient": &payment.GatewayError{Transient: true, Err: errors.New("down")},
		"permanent": &payment.GatewayError{Err: errors.New("refused")},
	}
	for op, fn := range call {
		for outcome, want := range outcomes {
			before := count(op, outcome)

			err := fn(Instrument(stubGateway{err: want}))

			assert.Equal(t, want, err, "%s %s: the error passes through", op, outcome)
			assert.Equal(t, before+1, count(op, outcome), "%s %s", op, outcome)
		}
	}
}

func TestInstrument_ReturnsTheValuesOfTheWrappedGateway(t *testing.T) {
	g := Instrument(stubGateway{})
	co, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{})
	require.NoError(t, err)
	assert.Equal(t, "pref", co.ExternalID)
	found, err := g.FindPayments(context.Background(), "x")
	require.NoError(t, err)
	assert.Len(t, found, 1)
	gp, err := g.GetPayment(context.Background(), "1")
	require.NoError(t, err)
	assert.Equal(t, "1", gp.ID)
	assert.Greater(t, testutil.CollectAndCount(metrics.GatewayRequestDuration), 0)
}
