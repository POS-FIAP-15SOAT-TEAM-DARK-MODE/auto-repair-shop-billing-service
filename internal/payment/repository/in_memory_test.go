package repository

import (
	"context"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemory(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()

	_, err := repo.FindLatestByServiceOrder(ctx, nil, "SO-1")
	assert.ErrorIs(t, err, payment.ErrNotFound)

	require.NoError(t, repo.Create(ctx, nil, &payment.Payment{ID: "p1", ServiceOrderID: "SO-1", Status: payment.StatusRejected}))
	require.NoError(t, repo.Create(ctx, nil, &payment.Payment{ID: "p2", ServiceOrderID: "SO-1", Status: payment.StatusPending}))

	latest, err := repo.FindLatestByServiceOrder(ctx, nil, "SO-1")
	require.NoError(t, err)
	assert.Equal(t, "p2", latest.ID)

	got, err := repo.FindByID(ctx, nil, "p1")
	require.NoError(t, err)
	assert.Equal(t, payment.StatusRejected, got.Status)

	_, err = repo.FindByID(ctx, nil, "nope")
	assert.ErrorIs(t, err, payment.ErrNotFound)

	require.NoError(t, repo.Update(ctx, nil, &payment.Payment{ID: "p2", Status: payment.StatusApproved}))
	latest, _ = repo.FindLatestByServiceOrder(ctx, nil, "SO-1")
	assert.Equal(t, payment.StatusApproved, latest.Status)

	assert.ErrorIs(t, repo.Update(ctx, nil, &payment.Payment{ID: "nope"}), payment.ErrNotFound)
}

func TestInMemory_UpdateKeepsTheGatewayFieldsAndListWorkFilters(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	mp := func(id string, st payment.Status) *payment.Payment {
		return &payment.Payment{ID: id, ServiceOrderID: id, Provider: payment.ProviderMercadoPago, Status: st, ExpiresAt: &future}
	}
	require.NoError(t, repo.Create(ctx, nil, &payment.Payment{ID: "internal", Provider: payment.ProviderName, Status: payment.StatusPending}))
	require.NoError(t, repo.Create(ctx, nil, mp("new", payment.StatusPending)))  // needs a checkout
	require.NoError(t, repo.Create(ctx, nil, mp("open", payment.StatusPending))) // has a checkout
	expired := mp("expired", payment.StatusPending)
	expired.ExpiresAt = &past
	require.NoError(t, repo.Create(ctx, nil, expired))
	require.NoError(t, repo.Create(ctx, nil, mp("paid", payment.StatusApproved)))
	require.NoError(t, repo.Create(ctx, nil, mp("done", payment.StatusRejected)))

	open, _ := repo.FindByID(ctx, nil, "open")
	open.ExternalID, open.CheckoutURL = "pref-open", "https://mp/open"
	require.NoError(t, repo.Update(ctx, nil, open))
	paid, _ := repo.FindByID(ctx, nil, "paid")
	paid.ExternalID, paid.GatewayPaymentID, paid.RefundRequestedAt = "pref-paid", "1001", &now
	require.NoError(t, repo.Update(ctx, nil, paid))
	done, _ := repo.FindByID(ctx, nil, "done")
	done.ExternalID = "pref-done"
	require.NoError(t, repo.Update(ctx, nil, done))

	ids := func(kind payment.WorkKind, limit int) []string {
		list, err := repo.ListWork(ctx, nil, kind, now, limit)
		require.NoError(t, err)
		var out []string
		for _, p := range list {
			out = append(out, p.ID)
		}
		return out
	}
	assert.Equal(t, []string{"new", "expired"}, ids(payment.WorkCreateCheckout, 0))
	assert.Equal(t, []string{"new"}, ids(payment.WorkCreateCheckout, 1))
	assert.Equal(t, []string{"open"}, ids(payment.WorkReconcile, 0))
	assert.Equal(t, []string{"expired"}, ids(payment.WorkExpire, 0))
	assert.Equal(t, []string{"paid"}, ids(payment.WorkRefund, 0))
	assert.Equal(t, []string{"done"}, ids(payment.WorkCloseCheckout, 0))
	assert.Empty(t, ids(payment.WorkKind("NOPE"), 0))

	got, _ := repo.FindByIDForUpdate(ctx, nil, "paid")
	assert.Equal(t, "1001", got.GatewayPaymentID)
	_, err := repo.FindByIDForUpdate(ctx, nil, "nope")
	assert.ErrorIs(t, err, payment.ErrNotFound)
}
