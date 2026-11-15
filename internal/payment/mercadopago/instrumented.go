package mercadopago

import (
	"context"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
)

// instrumented counts and times every call of a payment.Gateway.
type instrumented struct {
	next payment.Gateway
}

// Instrument wraps a gateway so every call feeds gateway_requests_total and
// gateway_request_duration_seconds.
func Instrument(next payment.Gateway) payment.Gateway { return &instrumented{next: next} }

func observe(op string, call func() error) error {
	start := time.Now()
	err := call()
	outcome := "ok"
	if err != nil {
		outcome = "permanent"
		if payment.IsTransient(err) {
			outcome = "transient"
		}
	}
	metrics.GatewayRequests.WithLabelValues(op, outcome).Inc()
	metrics.GatewayRequestDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
	return err
}

func (i *instrumented) CreateCheckout(ctx context.Context, req payment.CheckoutRequest) (co payment.Checkout, err error) {
	err = observe("create_checkout", func() error { co, err = i.next.CreateCheckout(ctx, req); return err })
	return co, err
}

func (i *instrumented) FindPayments(ctx context.Context, externalReference string) (found []payment.GatewayPayment, err error) {
	err = observe("find_payments", func() error { found, err = i.next.FindPayments(ctx, externalReference); return err })
	return found, err
}

func (i *instrumented) GetPayment(ctx context.Context, id string) (gp payment.GatewayPayment, err error) {
	err = observe("get_payment", func() error { gp, err = i.next.GetPayment(ctx, id); return err })
	return gp, err
}

func (i *instrumented) FindCheckout(ctx context.Context, paymentID string) (co payment.Checkout, found bool, err error) {
	err = observe("find_checkout", func() error { co, found, err = i.next.FindCheckout(ctx, paymentID); return err })
	return co, found, err
}

func (i *instrumented) Refund(ctx context.Context, id string) error {
	return observe("refund", func() error { return i.next.Refund(ctx, id) })
}

func (i *instrumented) CancelPayment(ctx context.Context, id string) error {
	return observe("cancel_payment", func() error { return i.next.CancelPayment(ctx, id) })
}

func (i *instrumented) ExpireCheckout(ctx context.Context, externalID string) error {
	return observe("expire_checkout", func() error { return i.next.ExpireCheckout(ctx, externalID) })
}
