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
}

// Transition moves the payment to the next status, or returns ErrInvalidTransition
// (repeated and invalid transitions are rejected, never applied).
func (p *Payment) Transition(to Status) error {
	if !slices.Contains(allowed[p.Status], to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, p.Status, to)
	}
	p.Status = to
	return nil
}

// Repository persists payments. ex is the caller's transaction (or *sql.DB for reads).
type Repository interface {
	Create(ctx context.Context, ex db.Executor, p *Payment) error
	FindByID(ctx context.Context, ex db.Executor, id string) (*Payment, error)
	// FindLatestByServiceOrder returns the most recently created payment of the service order.
	FindLatestByServiceOrder(ctx context.Context, ex db.Executor, serviceOrderID string) (*Payment, error)
	Update(ctx context.Context, ex db.Executor, p *Payment) error
}
