package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var payCols = []string{"id", "budget_id", "service_order_id", "saga_id", "amount", "status", "provider", "created_at", "updated_at",
	"external_id", "gateway_payment_id", "checkout_url", "expires_at", "refund_requested_at", "checkout_closed_at"}

func TestPostgres_Create(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("INSERT INTO payment").
		WithArgs("p1", "b1", "SO-1", "saga-1", "221.00", "PENDING", "internal", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	p := &payment.Payment{ID: "p1", BudgetID: "b1", ServiceOrderID: "SO-1", SagaID: "saga-1",
		Amount: decimal.RequireFromString("221"), Status: payment.StatusPending, Provider: "internal"}
	require.NoError(t, NewPostgres().Create(context.Background(), db, p))

	mock.ExpectExec("INSERT INTO payment").WillReturnError(errors.New("insert boom"))
	assert.EqualError(t, NewPostgres().Create(context.Background(), db, p), "insert boom")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_FindByID(t *testing.T) {
	now := time.Now()
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("FROM payment WHERE id").WithArgs("p1").
		WillReturnRows(sqlmock.NewRows(payCols).AddRow("p1", "b1", "SO-1", "saga-1", "221.00", "APPROVED", "internal", now, now, nil, nil, nil, nil, nil, nil))
	got, err := NewPostgres().FindByID(context.Background(), db, "p1")
	require.NoError(t, err)
	assert.Equal(t, payment.StatusApproved, got.Status)
	assert.Equal(t, "221", got.Amount.String())
	assert.Equal(t, "saga-1", got.SagaID)

	mock.ExpectQuery("FROM payment WHERE id").WillReturnRows(sqlmock.NewRows(payCols))
	_, err = NewPostgres().FindByID(context.Background(), db, "missing")
	assert.ErrorIs(t, err, payment.ErrNotFound)

	mock.ExpectQuery("FROM payment WHERE id").WillReturnError(errors.New("q boom"))
	_, err = NewPostgres().FindByID(context.Background(), db, "p1")
	assert.EqualError(t, err, "q boom")

	mock.ExpectQuery("FROM payment WHERE id").WillReturnRows(sqlmock.NewRows(payCols).AddRow("p1", "b1", "SO-1", "saga-1", "bad", "APPROVED", "internal", now, now, nil, nil, nil, nil, nil, nil))
	_, err = NewPostgres().FindByID(context.Background(), db, "p1")
	assert.Error(t, err)
}

func TestPostgres_FindLatestByServiceOrder(t *testing.T) {
	now := time.Now()
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("ORDER BY created_at DESC").WithArgs("SO-1").
		WillReturnRows(sqlmock.NewRows(payCols).AddRow("p2", "b1", "SO-1", "saga-1", "10.00", "PENDING", "internal", now, now, nil, nil, nil, nil, nil, nil))
	got, err := NewPostgres().FindLatestByServiceOrder(context.Background(), db, "SO-1")
	require.NoError(t, err)
	assert.Equal(t, "p2", got.ID)

	mock.ExpectQuery("ORDER BY created_at DESC").WillReturnRows(sqlmock.NewRows(payCols))
	_, err = NewPostgres().FindLatestByServiceOrder(context.Background(), db, "SO-9")
	assert.ErrorIs(t, err, payment.ErrNotFound)
}

func TestPostgres_Update(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE payment SET status").WithArgs("p1", "REFUNDED", nil, nil, nil, nil, nil).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, NewPostgres().Update(context.Background(), db, &payment.Payment{ID: "p1", Status: payment.StatusRefunded}))

	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	mock.ExpectExec("UPDATE payment SET status").WithArgs("p2", "APPROVED", "pref-1", "186237542593", "https://mp/init", at, at).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, NewPostgres().Update(context.Background(), db, &payment.Payment{
		ID: "p2", Status: payment.StatusApproved, ExternalID: "pref-1", GatewayPaymentID: "186237542593",
		CheckoutURL: "https://mp/init", RefundRequestedAt: &at, CheckoutClosedAt: &at}))

	mock.ExpectExec("UPDATE payment").WillReturnError(errors.New("upd boom"))
	assert.EqualError(t, NewPostgres().Update(context.Background(), db, &payment.Payment{ID: "p1"}), "upd boom")
}

func TestPostgres_CreateStoresTheExpiry(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	at := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	mock.ExpectExec("INSERT INTO payment").
		WithArgs("p1", "b1", "SO-1", "saga-1", "10.00", "PENDING", "mercadopago", at).
		WillReturnResult(sqlmock.NewResult(0, 1))
	p := &payment.Payment{ID: "p1", BudgetID: "b1", ServiceOrderID: "SO-1", SagaID: "saga-1",
		Amount: decimal.RequireFromString("10"), Status: payment.StatusPending, Provider: payment.ProviderMercadoPago, ExpiresAt: &at}
	require.NoError(t, NewPostgres().Create(context.Background(), db, p))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_FindByIDLoadsTheGatewayFields(t *testing.T) {
	now := time.Now()
	exp := now.Add(48 * time.Hour)
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("FROM payment WHERE id").WithArgs("p1").WillReturnRows(sqlmock.NewRows(payCols).
		AddRow("p1", "b1", "SO-1", "saga-1", "10.00", "PENDING", "mercadopago", now, now, "pref-1", "1001", "https://mp/init", exp, nil, nil))
	got, err := NewPostgres().FindByID(context.Background(), db, "p1")
	require.NoError(t, err)
	assert.Equal(t, "pref-1", got.ExternalID)
	assert.Equal(t, "1001", got.GatewayPaymentID)
	assert.Equal(t, "https://mp/init", got.CheckoutURL)
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, got.ExpiresAt.Equal(exp))
	assert.Nil(t, got.RefundRequestedAt)
	assert.True(t, got.GatewayManaged())
}

func TestPostgres_FindByIDForUpdateLocksTheRow(t *testing.T) {
	now := time.Now()
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("FROM payment WHERE id = \\$1 FOR UPDATE").WithArgs("p1").
		WillReturnRows(sqlmock.NewRows(payCols).AddRow("p1", "b1", "SO-1", "saga-1", "10.00", "PENDING", "internal", now, now, nil, nil, nil, nil, nil, nil))
	got, err := NewPostgres().FindByIDForUpdate(context.Background(), db, "p1")
	require.NoError(t, err)
	assert.Equal(t, "p1", got.ID)

	mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows(payCols))
	_, err = NewPostgres().FindByIDForUpdate(context.Background(), db, "missing")
	assert.ErrorIs(t, err, payment.ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_ListWork(t *testing.T) {
	now := time.Now()
	row := func(rows *sqlmock.Rows) *sqlmock.Rows {
		return rows.AddRow("p1", "b1", "SO-1", "saga-1", "10.00", "PENDING", "mercadopago", now, now, nil, nil, nil, nil, nil, nil)
	}
	tests := []struct {
		kind  payment.WorkKind
		match string
		args  []driver.Value
	}{
		{payment.WorkCreateCheckout, "status = 'PENDING' AND external_id IS NULL", []driver.Value{20}},
		{payment.WorkReconcile, "status = 'PENDING' AND external_id IS NOT NULL", []driver.Value{20}},
		{payment.WorkExpire, "expires_at < \\$2", []driver.Value{20, now}},
		{payment.WorkRefund, "status = 'APPROVED' AND refund_requested_at IS NOT NULL", []driver.Value{20}},
		{payment.WorkCloseCheckout, "checkout_closed_at IS NULL", []driver.Value{20}},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			mock.ExpectQuery(tt.match).WithArgs(tt.args...).WillReturnRows(row(sqlmock.NewRows(payCols)))
			got, err := NewPostgres().ListWork(context.Background(), db, tt.kind, now, 20)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "p1", got[0].ID)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}

	t.Run("errors", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		_, err := NewPostgres().ListWork(context.Background(), db, payment.WorkKind("NOPE"), now, 1)
		assert.Error(t, err)

		mock.ExpectQuery("FROM payment").WillReturnError(errors.New("q boom"))
		_, err = NewPostgres().ListWork(context.Background(), db, payment.WorkReconcile, now, 1)
		assert.EqualError(t, err, "q boom")

		mock.ExpectQuery("FROM payment").WillReturnRows(sqlmock.NewRows(payCols).
			AddRow("p1", "b1", "SO-1", "saga-1", "bad", "PENDING", "mercadopago", now, now, nil, nil, nil, nil, nil, nil))
		_, err = NewPostgres().ListWork(context.Background(), db, payment.WorkReconcile, now, 1)
		assert.Error(t, err)
	})
}
