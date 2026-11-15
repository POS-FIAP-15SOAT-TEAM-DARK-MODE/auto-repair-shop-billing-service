package payment

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// CheckoutRequest is what the gateway needs to open a hosted checkout for a payment.
type CheckoutRequest struct {
	PaymentID       string // sent as external_reference and used in the idempotency key
	Title           string
	Amount          decimal.Decimal
	NotificationURL string // empty means "do not send"
	ExpiresAt       time.Time
}

// Checkout is a hosted checkout opened at the gateway.
type Checkout struct {
	ExternalID string // gateway id of the checkout (the Mercado Pago preference id)
	URL        string // link the attendant gives to the customer (init_point)
}

// GatewayPayment is a payment held by the gateway, reduced to what Billing needs.
type GatewayPayment struct {
	ID                string
	Status            string // raw gateway status
	StatusDetail      string
	ExternalReference string          // our payment id
	Amount            decimal.Decimal // the price; interest the buyer pays on installments is not included
}

// Gateway is the port of a payment gateway that charges through a hosted checkout. The
// implementation is the only code that talks to the gateway; the domain never sees its types.
type Gateway interface {
	CreateCheckout(ctx context.Context, req CheckoutRequest) (Checkout, error)
	// FindPayments returns the gateway payments of an external reference.
	FindPayments(ctx context.Context, externalReference string) ([]GatewayPayment, error)
	GetPayment(ctx context.Context, gatewayPaymentID string) (GatewayPayment, error)
	Refund(ctx context.Context, gatewayPaymentID string) error
	// CancelPayment cancels a gateway payment that is not approved yet.
	CancelPayment(ctx context.Context, gatewayPaymentID string) error
	// FindCheckout returns the open checkout of a payment, found by its external reference. It is how a
	// checkout is recovered when its creation went through but the answer was lost before it was stored.
	FindCheckout(ctx context.Context, paymentID string) (Checkout, bool, error)
	// ExpireCheckout makes the checkout unpayable.
	ExpireCheckout(ctx context.Context, externalID string) error
}

// GatewayError wraps a failure of the gateway and says whether a retry can help. Network failures,
// 429 and 5xx are transient; the other 4xx answers are permanent.
type GatewayError struct {
	Transient bool
	NotFound  bool // the gateway answered 404
	Err       error
}

func (e *GatewayError) Error() string { return e.Err.Error() }
func (e *GatewayError) Unwrap() error { return e.Err }

// IsTransient reports whether err is a gateway failure worth retrying. Unknown errors count as transient.
func IsTransient(err error) bool {
	var ge *GatewayError
	if errors.As(err, &ge) {
		return ge.Transient
	}
	return true
}

// MapGatewayStatus converts a Mercado Pago payment status to a domain status. The bool is false for
// statuses that are not mapped (for example in_mediation): callers log and ignore them.
func MapGatewayStatus(gateway string) (Status, bool) {
	switch gateway {
	case "pending", "in_process", "authorized":
		return StatusPending, true
	case "approved":
		return StatusApproved, true
	case "rejected":
		return StatusRejected, true
	case "cancelled":
		return StatusCancelled, true
	case "refunded", "charged_back":
		return StatusRefunded, true
	default:
		return "", false
	}
}

// IsNotFound reports whether the gateway said it does not know the resource (404).
func IsNotFound(err error) bool {
	var ge *GatewayError
	return errors.As(err, &ge) && ge.NotFound
}
