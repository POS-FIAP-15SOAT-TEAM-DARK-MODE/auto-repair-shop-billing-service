package repository

import (
	"context"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

// InMemory implements budget.Repository for unit tests.
type InMemory struct {
	budgets map[string]*budget.Budget
	order   []string
}

func NewInMemory() *InMemory {
	return &InMemory{budgets: map[string]*budget.Budget{}}
}

func (r *InMemory) Create(_ context.Context, _ db.Executor, b *budget.Budget) error {
	cp := *b
	r.budgets[b.ID] = &cp
	r.order = append(r.order, b.ID)
	return nil
}

func (r *InMemory) FindGeneratedByServiceOrder(_ context.Context, _ db.Executor, serviceOrderID string) (*budget.Budget, error) {
	for _, id := range r.order {
		b := r.budgets[id]
		if b.ServiceOrderID == serviceOrderID && b.Status == budget.StatusGenerated {
			cp := *b
			return &cp, nil
		}
	}
	return nil, budget.ErrNotFound
}

func (r *InMemory) UpdateStatus(_ context.Context, _ db.Executor, id string, status budget.Status) error {
	b, ok := r.budgets[id]
	if !ok {
		return budget.ErrNotFound
	}
	b.Status = status
	return nil
}
