package uow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/uow"
	"github.com/stretchr/testify/assert"
)

func TestUnitOfWork_Execute_Success(t *testing.T) {
	startCalled := false
	successCalled := false
	u := &uow.UnitOfWork{
		OnStart: func(ctx context.Context) (context.Context, error) {
			startCalled = true
			return ctx, nil
		},
		OnSuccess: func(ctx context.Context) error {
			successCalled = true
			return nil
		},
	}

	err := u.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, startCalled)
	assert.True(t, successCalled)
}

func TestUnitOfWork_Execute_StepFailure(t *testing.T) {
	failureCalled := false
	expectedErr := errors.New("step failed")
	u := &uow.UnitOfWork{
		OnFailure: func(ctx context.Context, cause error) error {
			failureCalled = true
			assert.Equal(t, expectedErr, cause)
			return nil
		},
	}

	err := u.Execute(context.Background(), func(ctx context.Context) error {
		return expectedErr
	})

	assert.ErrorIs(t, err, expectedErr)
	assert.True(t, failureCalled)
}

func TestUnitOfWork_Execute_StartFailure(t *testing.T) {
	expectedErr := errors.New("start failed")
	u := &uow.UnitOfWork{
		OnStart: func(ctx context.Context) (context.Context, error) {
			return ctx, expectedErr
		},
	}

	err := u.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	assert.ErrorIs(t, err, expectedErr)
}

func TestUnitOfWork_Execute_RollbackFailure(t *testing.T) {
	stepErr := errors.New("step failed")
	rollbackErr := errors.New("rollback failed")
	u := &uow.UnitOfWork{
		OnFailure: func(ctx context.Context, cause error) error {
			return rollbackErr
		},
	}

	err := u.Execute(context.Background(), func(ctx context.Context) error {
		return stepErr
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), stepErr.Error())
	assert.Contains(t, err.Error(), rollbackErr.Error())
}

func TestUnitOfWork_Execute_NoHooks(t *testing.T) {
	u := &uow.UnitOfWork{}

	err := u.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
}

func TestUnitOfWork_Execute_NilHooksOnFailure(t *testing.T) {
	expectedErr := errors.New("step failed")
	u := &uow.UnitOfWork{
		OnFailure: nil,
	}

	err := u.Execute(context.Background(), func(ctx context.Context) error {
		return expectedErr
	})

	assert.Equal(t, expectedErr, err)
}
