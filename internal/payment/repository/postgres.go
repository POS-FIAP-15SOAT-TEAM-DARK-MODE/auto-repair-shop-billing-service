package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

const (
	insertQuery = `INSERT INTO payment (id, budget_id, service_order_id, saga_id, amount, status, provider) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	selectCols  = `SELECT id, budget_id, service_order_id, saga_id, amount, status, provider, created_at, updated_at FROM payment`
	byIDQuery   = selectCols + ` WHERE id = $1`
	latestQuery = selectCols + ` WHERE service_order_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`
	updateQuery = `UPDATE payment SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`
)

// Postgres implements payment.Repository on the billing database.
type Postgres struct{}

func NewPostgres() *Postgres { return &Postgres{} }

func (r *Postgres) Create(ctx context.Context, ex db.Executor, p *payment.Payment) error {
	_, err := ex.ExecContext(ctx, insertQuery, p.ID, p.BudgetID, p.ServiceOrderID, p.SagaID, p.Amount.StringFixed(2), string(p.Status), p.Provider)
	return err
}

func (r *Postgres) FindByID(ctx context.Context, ex db.Executor, id string) (*payment.Payment, error) {
	return scanOne(ex.QueryRowContext(ctx, byIDQuery, id))
}

func (r *Postgres) FindLatestByServiceOrder(ctx context.Context, ex db.Executor, serviceOrderID string) (*payment.Payment, error) {
	return scanOne(ex.QueryRowContext(ctx, latestQuery, serviceOrderID))
}

func (r *Postgres) Update(ctx context.Context, ex db.Executor, p *payment.Payment) error {
	_, err := ex.ExecContext(ctx, updateQuery, p.ID, string(p.Status))
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOne(row scanner) (*payment.Payment, error) {
	var p payment.Payment
	var amount string
	err := row.Scan(&p.ID, &p.BudgetID, &p.ServiceOrderID, &p.SagaID, &amount, &p.Status, &p.Provider, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, payment.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.Amount, err = decimal.NewFromString(amount); err != nil {
		return nil, err
	}
	return &p, nil
}
