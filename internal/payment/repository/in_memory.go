package repository

import (
	"context"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

// InMemory implements payment.Repository for unit tests. Insertion order is creation order.
type InMemory struct {
	items []*payment.Payment
}

func NewInMemory() *InMemory {
	return &InMemory{}
}

func (r *InMemory) Create(_ context.Context, _ db.Executor, p *payment.Payment) error {
	cp := *p
	r.items = append(r.items, &cp)
	return nil
}

func (r *InMemory) FindByID(_ context.Context, _ db.Executor, id string) (*payment.Payment, error) {
	for _, p := range r.items {
		if p.ID == id {
			cp := *p
			return &cp, nil
		}
	}
	return nil, payment.ErrNotFound
}

func (r *InMemory) FindLatestByServiceOrder(_ context.Context, _ db.Executor, serviceOrderID string) (*payment.Payment, error) {
	for i := len(r.items) - 1; i >= 0; i-- {
		if r.items[i].ServiceOrderID == serviceOrderID {
			cp := *r.items[i]
			return &cp, nil
		}
	}
	return nil, payment.ErrNotFound
}

func (r *InMemory) Update(_ context.Context, _ db.Executor, p *payment.Payment) error {
	for _, it := range r.items {
		if it.ID == p.ID {
			it.Status = p.Status
			return nil
		}
	}
	return payment.ErrNotFound
}
