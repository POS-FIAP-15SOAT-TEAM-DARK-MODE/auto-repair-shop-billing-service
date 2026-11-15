package db

import (
	"context"
	"database/sql"
)

// Executor is implemented by *sql.DB and *sql.Tx, so repositories run either
// standalone or inside a caller's transaction.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
