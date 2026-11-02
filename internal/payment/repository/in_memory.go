package repository

import (
	"context"
	"time"

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

// FindByIDForUpdate has no lock to take in memory.
func (r *InMemory) FindByIDForUpdate(ctx context.Context, ex db.Executor, id string) (*payment.Payment, error) {
	return r.FindByID(ctx, ex, id)
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

// Update stores the mutable fields, the same ones the Postgres repository writes.
func (r *InMemory) Update(_ context.Context, _ db.Executor, p *payment.Payment) error {
	for _, it := range r.items {
		if it.ID == p.ID {
			it.Status = p.Status
			it.ExternalID, it.GatewayPaymentID, it.CheckoutURL = p.ExternalID, p.GatewayPaymentID, p.CheckoutURL
			it.RefundRequestedAt, it.CheckoutClosedAt = p.RefundRequestedAt, p.CheckoutClosedAt
			return nil
		}
	}
	return payment.ErrNotFound
}

// ListWork mirrors the WHERE clauses of the Postgres repository.
func (r *InMemory) ListWork(_ context.Context, _ db.Executor, kind payment.WorkKind, now time.Time, limit int) ([]*payment.Payment, error) {
	var out []*payment.Payment
	for _, p := range r.items {
		if p.Provider != payment.ProviderMercadoPago || !matchesWork(p, kind, now) {
			continue
		}
		cp := *p
		out = append(out, &cp)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func matchesWork(p *payment.Payment, kind payment.WorkKind, now time.Time) bool {
	switch kind {
	case payment.WorkCreateCheckout:
		return p.Status == payment.StatusPending && p.ExternalID == ""
	case payment.WorkReconcile:
		return p.Status == payment.StatusPending && p.ExternalID != ""
	case payment.WorkExpire:
		return p.Status == payment.StatusPending && p.ExpiresAt != nil && p.ExpiresAt.Before(now)
	case payment.WorkRefund:
		return p.Status == payment.StatusApproved && p.RefundRequestedAt != nil
	case payment.WorkCloseCheckout:
		finished := p.Status == payment.StatusRejected || p.Status == payment.StatusCancelled || p.Status == payment.StatusRefunded
		return finished && p.ExternalID != "" && p.CheckoutClosedAt == nil
	}
	return false
}
