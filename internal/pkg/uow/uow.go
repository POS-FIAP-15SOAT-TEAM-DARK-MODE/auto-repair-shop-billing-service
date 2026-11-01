package uow

import (
	"context"
	"fmt"
)

//go:generate go run github.com/vektra/mockery/v2@latest --name=Executor --with-expecter
type Executor interface {
	Execute(ctx context.Context, steps ...Step) error
}

type Step func(context.Context) error

type Hook func(context.Context) (context.Context, error)

type CommitHook func(context.Context) error

type RollbackHook func(ctx context.Context, cause error) error

type UnitOfWork struct {
	OnStart Hook

	OnSuccess CommitHook

	OnFailure RollbackHook
}

func (u *UnitOfWork) Execute(ctx context.Context, steps ...Step) error {
	enriched, err := u.start(ctx)
	if err != nil {
		return err
	}

	for _, step := range steps {
		if err = step(enriched); err != nil {
			return u.fail(enriched, err)
		}
	}

	return u.succeed(enriched)
}

func (u *UnitOfWork) start(ctx context.Context) (context.Context, error) {
	if u.OnStart == nil {
		return ctx, nil
	}
	return u.OnStart(ctx)
}

func (u *UnitOfWork) succeed(ctx context.Context) error {
	if u.OnSuccess == nil {
		return nil
	}
	return u.OnSuccess(ctx)
}

func (u *UnitOfWork) fail(ctx context.Context, cause error) error {
	if u.OnFailure == nil {
		return cause
	}
	if rbErr := u.OnFailure(ctx, cause); rbErr != nil {
		return fmt.Errorf("%w (rollback error: %v)", cause, rbErr)
	}
	return cause
}
