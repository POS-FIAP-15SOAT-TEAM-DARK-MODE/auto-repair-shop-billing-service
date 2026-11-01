package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var payCols = []string{"id", "budget_id", "service_order_id", "saga_id", "amount", "status", "provider", "created_at", "updated_at"}

func TestPostgres_Create(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("INSERT INTO payment").
		WithArgs("p1", "b1", "SO-1", "saga-1", "221.00", "PENDING", "internal").
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
		WillReturnRows(sqlmock.NewRows(payCols).AddRow("p1", "b1", "SO-1", "saga-1", "221.00", "APPROVED", "internal", now, now))
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

	mock.ExpectQuery("FROM payment WHERE id").WillReturnRows(sqlmock.NewRows(payCols).AddRow("p1", "b1", "SO-1", "saga-1", "bad", "APPROVED", "internal", now, now))
	_, err = NewPostgres().FindByID(context.Background(), db, "p1")
	assert.Error(t, err)
}

func TestPostgres_FindLatestByServiceOrder(t *testing.T) {
	now := time.Now()
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery("ORDER BY created_at DESC").WithArgs("SO-1").
		WillReturnRows(sqlmock.NewRows(payCols).AddRow("p2", "b1", "SO-1", "saga-1", "10.00", "PENDING", "internal", now, now))
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
	mock.ExpectExec("UPDATE payment SET status").WithArgs("p1", "REFUNDED").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, NewPostgres().Update(context.Background(), db, &payment.Payment{ID: "p1", Status: payment.StatusRefunded}))

	mock.ExpectExec("UPDATE payment").WillReturnError(errors.New("upd boom"))
	assert.EqualError(t, NewPostgres().Update(context.Background(), db, &payment.Payment{ID: "p1"}), "upd boom")
}
