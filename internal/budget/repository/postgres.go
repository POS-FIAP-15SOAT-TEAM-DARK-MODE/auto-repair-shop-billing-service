package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

const (
	insertBudgetQuery = `INSERT INTO budget (id, service_order_id, customer_user_id, status, total_amount) VALUES ($1, $2, $3, $4, $5)`
	insertItemQuery   = `INSERT INTO budget_item (id, budget_id, kind, ref_id, name, quantity, unit_price) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	findBudgetQuery   = `SELECT id, service_order_id, customer_user_id, status, total_amount, created_at FROM budget WHERE service_order_id = $1 AND status = 'GENERATED'`
	findItemsQuery    = `SELECT id, kind, ref_id, name, quantity, unit_price FROM budget_item WHERE budget_id = $1 ORDER BY kind, name, id`
	updateStatusQuery = `UPDATE budget SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`
)

// Postgres implements budget.Repository on the budget database.
type Postgres struct{}

func NewPostgres() *Postgres { return &Postgres{} }

func (r *Postgres) Create(ctx context.Context, ex db.Executor, b *budget.Budget) error {
	if _, err := ex.ExecContext(ctx, insertBudgetQuery, b.ID, b.ServiceOrderID, b.CustomerUserID, string(b.Status), b.TotalAmount.StringFixed(2)); err != nil {
		return err
	}
	for _, it := range b.Items {
		if _, err := ex.ExecContext(ctx, insertItemQuery, it.ID, b.ID, string(it.Kind), it.RefID, it.Name, it.Quantity, it.UnitPrice.StringFixed(2)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Postgres) FindGeneratedByServiceOrder(ctx context.Context, ex db.Executor, serviceOrderID string) (*budget.Budget, error) {
	var b budget.Budget
	var total string
	err := ex.QueryRowContext(ctx, findBudgetQuery, serviceOrderID).Scan(&b.ID, &b.ServiceOrderID, &b.CustomerUserID, &b.Status, &total, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, budget.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if b.TotalAmount, err = decimal.NewFromString(total); err != nil {
		return nil, err
	}

	rows, err := ex.QueryContext(ctx, findItemsQuery, b.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it budget.Item
		var price string
		if err := rows.Scan(&it.ID, &it.Kind, &it.RefID, &it.Name, &it.Quantity, &price); err != nil {
			return nil, err
		}
		if it.UnitPrice, err = decimal.NewFromString(price); err != nil {
			return nil, err
		}
		b.Items = append(b.Items, it)
	}
	return &b, rows.Err()
}

func (r *Postgres) UpdateStatus(ctx context.Context, ex db.Executor, id string, status budget.Status) error {
	_, err := ex.ExecContext(ctx, updateStatusQuery, id, string(status))
	return err
}
