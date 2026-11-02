package repository

import (
	"context"
	"sort"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

// InMemory implements payment.Repository for unit tests. Insertion order is creation order.
type InMemory struct {
	items   []*payment.Payment
	checked map[string]time.Time

	unsettled        []*payment.Unsettled
	unsettledDone    map[string]bool
	unsettledChecked map[string]time.Time
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

// ListWork mirrors the WHERE and ORDER BY of the Postgres repository.
func (r *InMemory) ListWork(_ context.Context, _ db.Executor, kind payment.WorkKind, now time.Time, limit int) ([]*payment.Payment, error) {
	var out []*payment.Payment
	for _, p := range r.items {
		if p.Provider != payment.ProviderMercadoPago || !matchesWork(p, kind, now) {
			continue
		}
		if at, ok := r.checked[p.ID]; kind == payment.WorkReconcile && ok && !at.Before(now.Add(-payment.ReconcileEvery)) {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	// never checked first, then the oldest check; the sort is stable so creation order breaks ties
	sort.SliceStable(out, func(i, j int) bool {
		ai, iok := r.checked[out[i].ID]
		aj, jok := r.checked[out[j].ID]
		if iok != jok {
			return !iok
		}
		return ai.Before(aj)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MarkChecked remembers when the worker looked at the payment.
func (r *InMemory) MarkChecked(_ context.Context, _ db.Executor, id string, at time.Time) error {
	if r.checked == nil {
		r.checked = map[string]time.Time{}
	}
	r.checked[id] = at
	return nil
}

func matchesWork(p *payment.Payment, kind payment.WorkKind, now time.Time) bool {
	switch kind {
	case payment.WorkCreateCheckout:
		return p.Status == payment.StatusPending && p.ExternalID == ""
	case payment.WorkReconcile:
		return p.Status == payment.StatusPending // with or without a checkout: the gateway may hold one we never stored
	case payment.WorkExpire:
		return p.Status == payment.StatusPending && p.ExpiresAt != nil && p.ExpiresAt.Before(now)
	case payment.WorkRefund:
		return p.Status == payment.StatusApproved && p.RefundRequestedAt != nil
	case payment.WorkCloseCheckout:
		finished := p.Status == payment.StatusRejected || p.Status == payment.StatusCancelled || p.Status == payment.StatusRefunded
		return finished && p.CheckoutClosedAt == nil
	}
	return false
}

// PendingStats mirrors the query of the Postgres repository.
func (r *InMemory) PendingStats(_ context.Context, _ db.Executor) (payment.PendingStats, error) {
	var out payment.PendingStats
	for _, p := range r.items {
		if p.Provider != payment.ProviderMercadoPago {
			continue
		}
		switch {
		case p.Status == payment.StatusPending:
			out.Count++
			if out.Oldest == nil || p.CreatedAt.Before(*out.Oldest) {
				created := p.CreatedAt
				out.Oldest = &created
			}
		case p.Status == payment.StatusApproved && p.RefundRequestedAt != nil:
			out.RefundsPending++
			if out.OldestRefundRequest == nil || p.RefundRequestedAt.Before(*out.OldestRefundRequest) {
				requested := *p.RefundRequestedAt
				out.OldestRefundRequest = &requested
			}
		}
	}
	for _, u := range r.unsettled {
		if r.unsettledDone[u.GatewayPaymentID] {
			continue
		}
		out.UnsettledPending++
		if out.OldestUnsettled == nil || u.CreatedAt.Before(*out.OldestUnsettled) {
			created := u.CreatedAt
			out.OldestUnsettled = &created
		}
	}
	return out, nil
}

// RecordUnsettled mirrors the Postgres repository: a gateway payment is recorded once.
func (r *InMemory) RecordUnsettled(_ context.Context, _ db.Executor, u *payment.Unsettled) (bool, error) {
	for _, it := range r.unsettled {
		if it.GatewayPaymentID == u.GatewayPaymentID {
			return false, nil
		}
	}
	cp := *u
	r.unsettled = append(r.unsettled, &cp)
	return true, nil
}

// ListUnsettled returns the ones not refunded yet, the ones looked at least recently first.
func (r *InMemory) ListUnsettled(_ context.Context, _ db.Executor, limit int) ([]*payment.Unsettled, error) {
	var out []*payment.Unsettled
	for _, u := range r.unsettled {
		if !r.unsettledDone[u.GatewayPaymentID] {
			cp := *u
			out = append(out, &cp)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, iok := r.unsettledChecked[out[i].GatewayPaymentID]
		aj, jok := r.unsettledChecked[out[j].GatewayPaymentID]
		if iok != jok {
			return !iok
		}
		return ai.Before(aj)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MarkUnsettledChecked remembers when the worker looked at the unsettled payment.
func (r *InMemory) MarkUnsettledChecked(_ context.Context, _ db.Executor, gatewayPaymentID string, at time.Time) error {
	if r.unsettledChecked == nil {
		r.unsettledChecked = map[string]time.Time{}
	}
	r.unsettledChecked[gatewayPaymentID] = at
	return nil
}

// MarkUnsettledRefunded closes the record of an unsettled payment.
func (r *InMemory) MarkUnsettledRefunded(_ context.Context, _ db.Executor, gatewayPaymentID string, _ time.Time) error {
	if r.unsettledDone == nil {
		r.unsettledDone = map[string]bool{}
	}
	r.unsettledDone[gatewayPaymentID] = true
	return nil
}
