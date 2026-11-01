package uow

import (
	"context"
)

func NewInMemoryUoW() Executor {
	return &UnitOfWork{
		OnStart: func(ctx context.Context) (context.Context, error) {
			return ctx, nil
		},
		OnSuccess: func(context.Context) error {
			return nil
		},
		OnFailure: func(_ context.Context, cause error) error {
			return cause
		},
	}
}
