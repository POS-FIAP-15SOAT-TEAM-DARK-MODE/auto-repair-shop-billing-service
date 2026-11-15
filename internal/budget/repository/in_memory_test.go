package repository

import (
	"context"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemory(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemory()

	b := &budget.Budget{ID: "b1", ServiceOrderID: "SO-1", Status: budget.StatusGenerated, TotalAmount: decimal.NewFromInt(5)}
	require.NoError(t, repo.Create(ctx, nil, b))

	got, err := repo.FindGeneratedByServiceOrder(ctx, nil, "SO-1")
	require.NoError(t, err)
	assert.Equal(t, "b1", got.ID)

	_, err = repo.FindGeneratedByServiceOrder(ctx, nil, "SO-2")
	assert.ErrorIs(t, err, budget.ErrNotFound)

	require.NoError(t, repo.UpdateStatus(ctx, nil, "b1", budget.StatusCancelled))
	_, err = repo.FindGeneratedByServiceOrder(ctx, nil, "SO-1")
	assert.ErrorIs(t, err, budget.ErrNotFound)

	assert.ErrorIs(t, repo.UpdateStatus(ctx, nil, "missing", budget.StatusCancelled), budget.ErrNotFound)
}
