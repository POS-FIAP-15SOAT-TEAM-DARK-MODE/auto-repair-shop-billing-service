package repository

import (
	"context"
	"testing"

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
