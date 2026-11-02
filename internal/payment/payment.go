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

// GatewayManaged reports whether the payment already has a checkout at the gateway, so the
// gateway (not the attendant) is the source of truth for its status.
func (p *Payment) GatewayManaged() bool { return p.ExternalID != "" }

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
	// ListWork returns up to limit gateway payments of the given kind, oldest first. now is used by WorkExpire.
	ListWork(ctx context.Context, ex db.Executor, kind WorkKind, now time.Time, limit int) ([]*Payment, error)
}
