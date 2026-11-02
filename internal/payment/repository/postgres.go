package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
)

const (
	insertQuery = `INSERT INTO payment (id, budget_id, service_order_id, saga_id, amount, status, provider, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	selectCols  = `SELECT id, budget_id, service_order_id, saga_id, amount, status, provider, created_at, updated_at,
		external_id, gateway_payment_id, checkout_url, expires_at, refund_requested_at, checkout_closed_at FROM payment`
	byIDQuery       = selectCols + ` WHERE id = $1`
	byIDUpdateQuery = byIDQuery + ` FOR UPDATE`
	latestQuery     = selectCols + ` WHERE service_order_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`
	updateQuery     = `UPDATE payment SET status = $2, external_id = $3, gateway_payment_id = $4, checkout_url = $5,
		refund_requested_at = $6, checkout_closed_at = $7, updated_at = CURRENT_TIMESTAMP WHERE id = $1`

	workPrefix       = selectCols + ` WHERE provider = 'mercadopago' AND `
	workSuffix       = ` ORDER BY last_checked_at NULLS FIRST, created_at, id LIMIT $1`
	markCheckedQuery = `UPDATE payment SET last_checked_at = $2 WHERE id = $1`
)

// Postgres implements payment.Repository on the billing database.
type Postgres struct{}

func NewPostgres() *Postgres { return &Postgres{} }

func (r *Postgres) Create(ctx context.Context, ex db.Executor, p *payment.Payment) error {
	_, err := ex.ExecContext(ctx, insertQuery, p.ID, p.BudgetID, p.ServiceOrderID, p.SagaID, p.Amount.StringFixed(2), string(p.Status), p.Provider, nullTime(p.ExpiresAt))
	return err
}

func (r *Postgres) FindByID(ctx context.Context, ex db.Executor, id string) (*payment.Payment, error) {
	return scanOne(ex.QueryRowContext(ctx, byIDQuery, id))
}

func (r *Postgres) FindByIDForUpdate(ctx context.Context, ex db.Executor, id string) (*payment.Payment, error) {
	return scanOne(ex.QueryRowContext(ctx, byIDUpdateQuery, id))
}

// ListWork returns the gateway payments the worker has to act on. The WHERE clauses mirror payment.WorkKind.
func (r *Postgres) ListWork(ctx context.Context, ex db.Executor, kind payment.WorkKind, now time.Time, limit int) ([]*payment.Payment, error) {
	var query string
	args := []any{limit}
	switch kind {
	case payment.WorkCreateCheckout:
		query = workPrefix + `status = 'PENDING' AND external_id IS NULL` + workSuffix
	case payment.WorkReconcile:
		query = workPrefix + `status = 'PENDING' AND external_id IS NOT NULL AND (last_checked_at IS NULL OR last_checked_at < $2)` + workSuffix
		args = append(args, now.Add(-payment.ReconcileEvery))
	case payment.WorkExpire:
		query = workPrefix + `status = 'PENDING' AND expires_at IS NOT NULL AND expires_at < $2` + workSuffix
		args = append(args, now)
	case payment.WorkRefund:
		query = workPrefix + `status = 'APPROVED' AND refund_requested_at IS NOT NULL` + workSuffix
	case payment.WorkCloseCheckout:
		query = workPrefix + `status IN ('REJECTED', 'CANCELLED', 'REFUNDED') AND external_id IS NOT NULL AND checkout_closed_at IS NULL` + workSuffix
	default:
		return nil, fmt.Errorf("unknown work kind %q", kind)
	}

	rows, err := ex.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*payment.Payment
	for rows.Next() {
		p, err := scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Postgres) FindLatestByServiceOrder(ctx context.Context, ex db.Executor, serviceOrderID string) (*payment.Payment, error) {
	return scanOne(ex.QueryRowContext(ctx, latestQuery, serviceOrderID))
}

func (r *Postgres) Update(ctx context.Context, ex db.Executor, p *payment.Payment) error {
	_, err := ex.ExecContext(ctx, updateQuery, p.ID, string(p.Status), nullString(p.ExternalID), nullString(p.GatewayPaymentID),
		nullString(p.CheckoutURL), nullTime(p.RefundRequestedAt), nullTime(p.CheckoutClosedAt))
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOne(row scanner) (*payment.Payment, error) {
	var p payment.Payment
	var amount string
	var externalID, gatewayPaymentID, checkoutURL sql.NullString
	var expiresAt, refundRequestedAt, checkoutClosedAt sql.NullTime
	err := row.Scan(&p.ID, &p.BudgetID, &p.ServiceOrderID, &p.SagaID, &amount, &p.Status, &p.Provider, &p.CreatedAt, &p.UpdatedAt,
		&externalID, &gatewayPaymentID, &checkoutURL, &expiresAt, &refundRequestedAt, &checkoutClosedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, payment.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.Amount, err = decimal.NewFromString(amount); err != nil {
		return nil, err
	}
	p.ExternalID, p.GatewayPaymentID, p.CheckoutURL = externalID.String, gatewayPaymentID.String, checkoutURL.String
	p.ExpiresAt, p.RefundRequestedAt, p.CheckoutClosedAt = timePtr(expiresAt), timePtr(refundRequestedAt), timePtr(checkoutClosedAt)
	return &p, nil
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func timePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// MarkChecked stores when the gateway worker last looked at the payment.
func (r *Postgres) MarkChecked(ctx context.Context, ex db.Executor, id string, at time.Time) error {
	_, err := ex.ExecContext(ctx, markCheckedQuery, id, at)
	return err
}
