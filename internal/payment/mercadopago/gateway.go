// Package mercadopago implements payment.Gateway with the official SDK. It is the only package
// that imports the SDK or sees its types.
package mercadopago

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mercadopago/sdk-go/pkg/config"
	"github.com/mercadopago/sdk-go/pkg/mperror"
	mppayment "github.com/mercadopago/sdk-go/pkg/payment"
	"github.com/mercadopago/sdk-go/pkg/preference"
	"github.com/mercadopago/sdk-go/pkg/refund"
	"github.com/mercadopago/sdk-go/pkg/requestoptions"
	"github.com/shopspring/decimal"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
)

// Gateway talks to Mercado Pago. Pass config.WithHTTPClient in tests to replace the transport.
type Gateway struct {
	preferences preference.Client
	payments    mppayment.Client
	refunds     refund.Client
}

var _ payment.Gateway = (*Gateway)(nil)

func New(accessToken string, opts ...config.Option) (*Gateway, error) {
	cfg, err := config.New(accessToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("mercadopago config: %w", err)
	}
	return &Gateway{
		preferences: preference.NewClient(cfg),
		payments:    mppayment.NewClient(cfg),
		refunds:     refund.NewClient(cfg),
	}, nil
}

// CreateCheckout opens a Checkout Pro preference with one item whose price is the payment amount.
// The SDK models unit_price as float64, so the conversion happens here and nowhere else. The
// idempotency key is derived from the payment id, so a retry never creates a second preference.
func (g *Gateway) CreateCheckout(ctx context.Context, req payment.CheckoutRequest) (payment.Checkout, error) {
	expires := req.ExpiresAt.UTC()
	r := preference.Request{
		Items: []preference.ItemRequest{{
			Title:      req.Title,
			Quantity:   1,
			UnitPrice:  req.Amount.InexactFloat64(),
			CurrencyID: "BRL",
		}},
		ExternalReference: req.PaymentID,
		NotificationURL:   req.NotificationURL,
		Expires:           true,
		ExpirationDateTo:  &expires,
	}
	resp, err := g.preferences.Create(requestoptions.WithIdempotencyKey(ctx, "checkout-"+req.PaymentID), r)
	if err != nil {
		return payment.Checkout{}, classify("create preference", err)
	}
	return payment.Checkout{ExternalID: resp.ID, URL: resp.InitPoint}, nil
}

func (g *Gateway) FindPayments(ctx context.Context, externalReference string) ([]payment.GatewayPayment, error) {
	resp, err := g.payments.Search(ctx, mppayment.SearchRequest{
		Limit: 30,
		Filters: map[string]string{
			"external_reference": externalReference,
			"sort":               "date_created",
			"criteria":           "desc",
		},
	})
	if err != nil {
		return nil, classify("search payments", err)
	}
	out := make([]payment.GatewayPayment, 0, len(resp.Results))
	for _, r := range resp.Results {
		out = append(out, toGatewayPayment(r))
	}
	return out, nil
}

func (g *Gateway) GetPayment(ctx context.Context, gatewayPaymentID string) (payment.GatewayPayment, error) {
	id, err := numericID(gatewayPaymentID)
	if err != nil {
		return payment.GatewayPayment{}, err
	}
	resp, err := g.payments.Get(ctx, id)
	if err != nil {
		return payment.GatewayPayment{}, classify("get payment", err)
	}
	return toGatewayPayment(*resp), nil
}

func (g *Gateway) Refund(ctx context.Context, gatewayPaymentID string) error {
	id, err := numericID(gatewayPaymentID)
	if err != nil {
		return err
	}
	if _, err := g.refunds.Create(requestoptions.WithIdempotencyKey(ctx, "refund-"+gatewayPaymentID), id); err != nil {
		return classify("refund payment", err)
	}
	return nil
}

func (g *Gateway) CancelPayment(ctx context.Context, gatewayPaymentID string) error {
	id, err := numericID(gatewayPaymentID)
	if err != nil {
		return err
	}
	if _, err := g.payments.Cancel(ctx, id); err != nil {
		return classify("cancel payment", err)
	}
	return nil
}

// ExpireCheckout updates the preference with an expiration date in the past; the checkout page then
// answers that what the customer wanted to pay is no longer available.
func (g *Gateway) ExpireCheckout(ctx context.Context, externalID string) error {
	past := time.Now().UTC().Add(-time.Minute)
	if _, err := g.preferences.Update(ctx, externalID, preference.Request{Expires: true, ExpirationDateTo: &past}); err != nil {
		return classify("expire preference", err)
	}
	return nil
}

func toGatewayPayment(r mppayment.Response) payment.GatewayPayment {
	return payment.GatewayPayment{
		ID:                strconv.Itoa(r.ID),
		Status:            r.Status,
		StatusDetail:      r.StatusDetail,
		ExternalReference: r.ExternalReference,
		Amount:            decimal.NewFromFloat(r.TransactionAmount).Round(2),
	}
}

func numericID(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, &payment.GatewayError{Err: fmt.Errorf("invalid gateway payment id %q", id)}
	}
	return n, nil
}

// classify wraps an SDK error with context and the transient/permanent verdict. Answers with a
// status code are permanent unless 429 or 5xx; anything without one (network, timeout) is transient.
func classify(op string, err error) error {
	transient := true
	var re *mperror.ResponseError
	if errors.As(err, &re) {
		transient = isTransientStatus(re.StatusCode)
	}
	return &payment.GatewayError{Transient: transient, Err: fmt.Errorf("%s: %w", op, err)}
}

// isTransientStatus says which HTTP answers are worth retrying. Besides 429 and 5xx, a 401 or 403 (a
// token that was rotated or lacks a permission), a 408 and a 409 (an idempotent call still running) do
// not say anything about the request itself, so they are retried and the payment is not failed for a
// problem of configuration. The other 4xx answers (400, 404, 422...) mean the request is wrong.
func isTransientStatus(status int) bool {
	switch status {
	case 401, 403, 408, 409, 429:
		return true
	}
	return status >= 500
}
