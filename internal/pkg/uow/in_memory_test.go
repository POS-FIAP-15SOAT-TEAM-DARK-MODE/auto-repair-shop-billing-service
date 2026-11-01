package uow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/uow"
	"github.com/stretchr/testify/assert"
)

func TestNewInMemoryUoW(t *testing.T) {
	unitOfWork := uow.NewInMemoryUoW()
	assert.NotNil(t, unitOfWork)

	ctx := context.Background()

	// Test success
	err := unitOfWork.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	assert.NoError(t, err)

	// Test failure
	expectedErr := errors.New("boom")
	err = unitOfWork.Execute(ctx, func(ctx context.Context) error {
		return expectedErr
	})
	assert.ErrorIs(t, err, expectedErr)
}
