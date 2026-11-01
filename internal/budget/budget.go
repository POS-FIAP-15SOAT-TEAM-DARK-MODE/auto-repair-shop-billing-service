package budget

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

type Status string

const (
	StatusGenerated Status = "GENERATED"
	StatusCancelled Status = "CANCELLED"
)

type Kind string

const (
	KindWork   Kind = "WORK"
	KindSupply Kind = "SUPPLY"
)

var (
	ErrNotFound         = errors.New("budget not found")
	ErrNoWorks          = errors.New("budget must have at least one work")
	ErrCustomerRequired = errors.New("customer user id is required")
	ErrNegativePrice    = errors.New("unit price cannot be negative")
	ErrInvalidQuantity  = errors.New("quantity must be greater than zero")
)

// Budget is the quote generated for a service order. Money is decimal, never float.
type Budget struct {
	ID             string
	ServiceOrderID string
	CustomerUserID string
	Status         Status
	TotalAmount    decimal.Decimal
	Items          []Item
	CreatedAt      time.Time
}

type Item struct {
	ID        string
	Kind      Kind
	RefID     string
	Name      string
	Quantity  int
	UnitPrice decimal.Decimal
}

type WorkLine struct {
	WorkID    string
	Name      string
	UnitPrice decimal.Decimal
}

type SupplyLine struct {
	SupplyID  string
	Name      string
	Quantity  int
	UnitPrice decimal.Decimal
}

// NewBudget validates the lines, rounds unit prices to 2 decimals and computes
// total = sum(work.unit_price) + sum(supply.unit_price * quantity).
// newID generates the ids of the budget and its items.
func NewBudget(newID func() string, serviceOrderID, customerUserID string, works []WorkLine, supplies []SupplyLine) (*Budget, error) {
	if customerUserID == "" {
		return nil, ErrCustomerRequired
	}
	if len(works) == 0 {
		return nil, ErrNoWorks
	}

	b := &Budget{
		ID:             newID(),
		ServiceOrderID: serviceOrderID,
		CustomerUserID: customerUserID,
		Status:         StatusGenerated,
		TotalAmount:    decimal.Zero,
	}

	for _, w := range works {
		price, err := normalizePrice(w.UnitPrice)
		if err != nil {
			return nil, err
		}
		b.Items = append(b.Items, Item{ID: newID(), Kind: KindWork, RefID: w.WorkID, Name: w.Name, Quantity: 1, UnitPrice: price})
		b.TotalAmount = b.TotalAmount.Add(price)
	}

	for _, s := range supplies {
		if s.Quantity <= 0 {
			return nil, ErrInvalidQuantity
		}
		price, err := normalizePrice(s.UnitPrice)
		if err != nil {
			return nil, err
		}
		b.Items = append(b.Items, Item{ID: newID(), Kind: KindSupply, RefID: s.SupplyID, Name: s.Name, Quantity: s.Quantity, UnitPrice: price})
		b.TotalAmount = b.TotalAmount.Add(price.Mul(decimal.NewFromInt(int64(s.Quantity))))
	}

	b.TotalAmount = b.TotalAmount.Round(2)
	return b, nil
}

// Cancel moves a GENERATED budget to CANCELLED. It reports whether the status changed.
func (b *Budget) Cancel() bool {
	if b.Status != StatusGenerated {
		return false
	}
	b.Status = StatusCancelled
	return true
}

func normalizePrice(p decimal.Decimal) (decimal.Decimal, error) {
	if p.IsNegative() {
		return decimal.Zero, ErrNegativePrice
	}
	return p.Round(2), nil
}

// Repository persists budgets. ex is the caller's transaction (or *sql.DB for reads).
type Repository interface {
	Create(ctx context.Context, ex db.Executor, b *Budget) error
	FindGeneratedByServiceOrder(ctx context.Context, ex db.Executor, serviceOrderID string) (*Budget, error)
	UpdateStatus(ctx context.Context, ex db.Executor, id string, status Status) error
}
