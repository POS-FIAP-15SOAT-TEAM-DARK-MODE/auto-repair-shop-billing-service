package uow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	pglib "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db/postgres"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"go.uber.org/zap"
)

type txKey struct{}

var ErrMissingPostgresTransaction = errors.New("transaction not found")

func NewTransactionalUoW(db *sql.DB) Executor {
	return &UnitOfWork{
		OnStart:   onStart(db),
		OnSuccess: onSuccess,
		OnFailure: onFailure,
	}
}

func onStart(db *sql.DB) func(context.Context) (context.Context, error) {
	return func(ctx context.Context) (context.Context, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			logger.Global().Debug("failed to begin transaction", zap.String("operation", "uow_onStart"), zap.Error(err))
			return ctx, pglib.Error(ctx, fmt.Errorf("begin tx: %w", err))
		}
		logger.Global().Debug("transaction started", zap.String("operation", "uow_onStart"))
		return withTx(ctx, tx), nil
	}
}

func onSuccess(ctx context.Context) error {
	tx, found := txFrom(ctx)
	if !found {
		logger.Global().Debug("postgres transaction not found on success", zap.String("operation", "uow_onSuccess"))
		return ErrMissingPostgresTransaction
	}
	if err := tx.Commit(); err != nil {
		logger.Global().Debug("failed to commit transaction", zap.String("operation", "uow_onSuccess"), zap.Error(err))
		return err
	}
	logger.Global().Debug("transaction committed successfully", zap.String("operation", "uow_onSuccess"))
	return nil
}

func onFailure(ctx context.Context, _ error) error {
	tx, found := txFrom(ctx)
	if !found {
		logger.Global().Debug("postgres transaction not found on failure", zap.String("operation", "uow_onFailure"))
		return ErrMissingPostgresTransaction
	}
	if err := tx.Rollback(); err != nil {
		logger.Global().Debug("failed to rollback transaction", zap.String("operation", "uow_onFailure"), zap.Error(err))
		return err
	}
	logger.Global().Debug("transaction rolled back", zap.String("operation", "uow_onFailure"))
	return nil
}

func withTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func txFrom(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	return tx, tx != nil && ok
}

func GetTransaction(ctx context.Context) (*sql.Tx, error) {
	tx, found := txFrom(ctx)
	if !found {
		logger.Global().Debug("postgres transaction not found when requested", zap.String("operation", "uow_GetTransaction"))
		return nil, ErrMissingPostgresTransaction
	}
	return tx, nil
}

func GetOneTimeTransaction(_ context.Context) (*sql.DB, error) {
	db := app.DBConnect()
	return db, nil
}
