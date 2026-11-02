package payment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/shopspring/decimal"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusApproved  Status = "APPROVED"
	StatusRejected  Status = "REJECTED"
	StatusRefunded  Status = "REFUNDED"
	StatusCancelled Status = "CANCELLED"
)

// ProviderName is stored in payment.provider for the built-in provider.
const ProviderName = "internal"

// ProviderMercadoPago is stored in payment.provider for payments charged through Mercado Pago.
const ProviderMercadoPago = "mercadopago"

var (
	ErrNotFound          = errors.New("payment not found")
	ErrInvalidTransition = errors.New("invalid payment status transition")
	// ErrGatewayManaged is returned when the attendant tries to confirm or reject a payment that has a
	// checkout at the gateway: the gateway is the source of truth for its status.
	ErrGatewayManaged = errors.New("payment is managed by the payment gateway")
	// ErrInvalidSignature is returned for a webhook whose x-signature does not verify.
	ErrInvalidSignature = errors.New("invalid webhook signature")
	// ErrInvalidNotification is returned for a webhook without a usable payment id.
	ErrInvalidNotification = errors.New("invalid webhook notification")
)

// allowed lists the valid next states of each status.
var allowed = map[Status][]Status{
	StatusPending:  {StatusApproved, StatusRejected, StatusCancelled},
	StatusApproved: {StatusRefunded},
}

type Payment struct {
	ID             string
	BudgetID       string
	ServiceOrderID string
	SagaID         string
	Amount         decimal.Decimal
	Status         Status
	Provider       string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// Gateway fields, empty for the internal provider.
	ExternalID        string     // checkout (preference) id at the gateway
	GatewayPaymentID  string     // id of the gateway payment that decided the outcome
	CheckoutURL       string     // link the attendant gives to the customer
	ExpiresAt         *time.Time // when the checkout stops being payable
	RefundRequestedAt *time.Time // set by RefundPayment on an APPROVED gateway payment
	CheckoutClosedAt  *time.Time // when the checkout was expired at the gateway
}

// ReconcileEvery is the minimum time between two looks of the reconciliation at the same payment.
const ReconcileEvery = 30 * time.Second

// GatewayManaged reports whether the payment is charged through the gateway, so the
// gateway (not the attendant) is the source of truth for its status.
func (p *Payment) GatewayManaged() bool { return p.Provider == ProviderMercadoPago }

// Transition moves the payment to the next status, or returns ErrInvalidTransition
// (repeated and invalid transitions are rejected, never applied).
func (p *Payment) Transition(to Status) error {
	if !slices.Contains(allowed[p.Status], to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, p.Status, to)
	}
	p.Status = to
	return nil
}

// WorkKind names a group of gateway payments that need attention from the gateway worker.
type WorkKind string

const (
	// WorkCreateCheckout: PENDING payments that still have no checkout.
	WorkCreateCheckout WorkKind = "CREATE_CHECKOUT"
	// WorkReconcile: PENDING payments that have a checkout and may have been paid.
	WorkReconcile WorkKind = "RECONCILE"
	// WorkExpire: PENDING payments whose checkout lifetime has passed.
	WorkExpire WorkKind = "EXPIRE"
	// WorkRefund: APPROVED payments with a refund requested.
	WorkRefund WorkKind = "REFUND"
	// WorkCloseCheckout: finished payments whose checkout is still open at the gateway.
	WorkCloseCheckout WorkKind = "CLOSE_CHECKOUT"
)

// Repository persists payments. ex is the caller's transaction (or *sql.DB for reads).
type Repository interface {
	Create(ctx context.Context, ex db.Executor, p *Payment) error
	FindByID(ctx context.Context, ex db.Executor, id string) (*Payment, error)
	// FindByIDForUpdate is FindByID that locks the row until the transaction ends, so concurrent
	// writers (webhook, worker, manual endpoints) cannot overwrite each other.
	FindByIDForUpdate(ctx context.Context, ex db.Executor, id string) (*Payment, error)
	// FindLatestByServiceOrder returns the most recently created payment of the service order.
	FindLatestByServiceOrder(ctx context.Context, ex db.Executor, serviceOrderID string) (*Payment, error)
	Update(ctx context.Context, ex db.Executor, p *Payment) error
	// ListWork returns up to limit gateway payments of the given kind, the ones looked at least recently
	// first (never looked at, then the oldest check), so a failing payment cannot starve the others. now is
	// used by WorkExpire and WorkReconcile (which waits ReconcileEvery between two looks at a payment).
	ListWork(ctx context.Context, ex db.Executor, kind WorkKind, now time.Time, limit int) ([]*Payment, error)
	// PendingStats describes the Mercado Pago payments in PENDING.
	PendingStats(ctx context.Context, ex db.Executor) (PendingStats, error)
	// MarkChecked records that the gateway worker looked at the payment at the given time.
	MarkChecked(ctx context.Context, ex db.Executor, id string, at time.Time) error

	// RecordUnsettled stores an approved gateway payment that must be refunded. It reports whether the
	// row is new: the same gateway payment is never recorded twice.
	RecordUnsettled(ctx context.Context, ex db.Executor, u *Unsettled) (bool, error)
	// ListUnsettled returns up to limit unsettled payments still to be refunded, the ones looked at least
	// recently first.
	ListUnsettled(ctx context.Context, ex db.Executor, limit int) ([]*Unsettled, error)
	// MarkUnsettledChecked records that the worker looked at the unsettled payment at the given time.
	MarkUnsettledChecked(ctx context.Context, ex db.Executor, gatewayPaymentID string, at time.Time) error
	// MarkUnsettledRefunded closes the record once the gateway no longer holds the money.
	MarkUnsettledRefunded(ctx context.Context, ex db.Executor, gatewayPaymentID string, at time.Time) error
}

// UnsettledReason says why an approved gateway payment was not settled.
type UnsettledReason string

const (
	// UnsettledLateApproval: the customer paid after the payment was cancelled or rejected.
	UnsettledLateApproval UnsettledReason = "LATE_APPROVAL"
	// UnsettledExtraApproval: a second approved payment for a payment already approved.
	UnsettledExtraApproval UnsettledReason = "EXTRA_APPROVAL"
)

// Unsettled is an approved payment at the gateway that Billing did not settle and has to refund.
type Unsettled struct {
	GatewayPaymentID string
	PaymentID        string
	Reason           UnsettledReason
	Amount           decimal.Decimal
	CreatedAt        time.Time
}

// PendingStats is what the worker publishes about the Mercado Pago payments waiting to be settled.
type PendingStats struct {
	Count  int
	Oldest *time.Time // creation time of the oldest one; nil when there is none

	// Refunds waiting for the gateway: approved payments whose refund was requested and not confirmed yet.
	RefundsPending      int
	OldestRefundRequest *time.Time // when the oldest of them was requested; nil when there is none

	// Unsettled payments waiting to be refunded (see Unsettled).
	UnsettledPending int
	OldestUnsettled  *time.Time // when the oldest of them was recorded; nil when there is none
}
