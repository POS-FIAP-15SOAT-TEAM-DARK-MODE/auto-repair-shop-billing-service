package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var budgetCols = []string{"id", "service_order_id", "customer_user_id", "status", "total_amount", "created_at"}
var itemCols = []string{"id", "kind", "ref_id", "name", "quantity", "unit_price"}

func TestPostgres_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	b := &budget.Budget{
		ID: "b1", ServiceOrderID: "SO-1", CustomerUserID: "c1", Status: budget.StatusGenerated,
		TotalAmount: decimal.RequireFromString("221.00"),
		Items: []budget.Item{
			{ID: "i1", Kind: budget.KindWork, RefID: "w1", Name: "Oil", Quantity: 1, UnitPrice: decimal.RequireFromString("150")},
			{ID: "i2", Kind: budget.KindSupply, RefID: "s1", Name: "Filter", Quantity: 2, UnitPrice: decimal.RequireFromString("35.5")},
		},
	}

	mock.ExpectExec("INSERT INTO budget ").WithArgs("b1", "SO-1", "c1", "GENERATED", "221.00").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO budget_item").WithArgs("i1", "b1", "WORK", "w1", "Oil", 1, "150.00").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO budget_item").WithArgs("i2", "b1", "SUPPLY", "s1", "Filter", 2, "35.50").WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewPostgres().Create(context.Background(), db, b))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_CreateErrors(t *testing.T) {
	b := &budget.Budget{ID: "b1", Items: []budget.Item{{ID: "i1", Kind: budget.KindWork, UnitPrice: decimal.Zero}}}

	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("INSERT INTO budget ").WillReturnError(errors.New("header boom"))
	assert.EqualError(t, NewPostgres().Create(context.Background(), db, b), "header boom")

	db2, mock2, _ := sqlmock.New()
	defer db2.Close()
	mock2.ExpectExec("INSERT INTO budget ").WillReturnResult(sqlmock.NewResult(0, 1))
	mock2.ExpectExec("INSERT INTO budget_item").WillReturnError(errors.New("item boom"))
	assert.EqualError(t, NewPostgres().Create(context.Background(), db2, b), "item boom")
}

func TestPostgres_FindGeneratedByServiceOrder(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	now := time.Now()

	mock.ExpectQuery("FROM budget WHERE service_order_id").WithArgs("SO-1").
		WillReturnRows(sqlmock.NewRows(budgetCols).AddRow("b1", "SO-1", "c1", "GENERATED", "221.00", now))
	mock.ExpectQuery("FROM budget_item").WithArgs("b1").
		WillReturnRows(sqlmock.NewRows(itemCols).AddRow("i1", "WORK", "w1", "Oil", 1, "150.00").AddRow("i2", "SUPPLY", "s1", "Filter", 2, "35.50"))

	got, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
	require.NoError(t, err)
	assert.Equal(t, "b1", got.ID)
	assert.Equal(t, "221", got.TotalAmount.String())
	require.Len(t, got.Items, 2)
	assert.Equal(t, budget.KindSupply, got.Items[1].Kind)
	assert.Equal(t, "35.5", got.Items[1].UnitPrice.String())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgres_FindGeneratedByServiceOrder_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("FROM budget WHERE").WillReturnRows(sqlmock.NewRows(budgetCols))

	_, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
	assert.ErrorIs(t, err, budget.ErrNotFound)
}

func TestPostgres_FindGeneratedByServiceOrder_Errors(t *testing.T) {
	now := time.Now()

	t.Run("header query fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("FROM budget WHERE").WillReturnError(errors.New("q boom"))
		_, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
		assert.EqualError(t, err, "q boom")
	})

	t.Run("bad total decimal", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("FROM budget WHERE").WillReturnRows(sqlmock.NewRows(budgetCols).AddRow("b1", "SO-1", "c1", "GENERATED", "abc", now))
		_, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
		assert.Error(t, err)
	})

	t.Run("items query fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("FROM budget WHERE").WillReturnRows(sqlmock.NewRows(budgetCols).AddRow("b1", "SO-1", "c1", "GENERATED", "1.00", now))
		mock.ExpectQuery("FROM budget_item").WillReturnError(errors.New("items boom"))
		_, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
		assert.EqualError(t, err, "items boom")
	})

	t.Run("item scan fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("FROM budget WHERE").WillReturnRows(sqlmock.NewRows(budgetCols).AddRow("b1", "SO-1", "c1", "GENERATED", "1.00", now))
		mock.ExpectQuery("FROM budget_item").WillReturnRows(sqlmock.NewRows(itemCols).AddRow("i1", "WORK", "w1", "Oil", "x", "1.00"))
		_, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
		assert.Error(t, err)
	})

	t.Run("bad item price", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("FROM budget WHERE").WillReturnRows(sqlmock.NewRows(budgetCols).AddRow("b1", "SO-1", "c1", "GENERATED", "1.00", now))
		mock.ExpectQuery("FROM budget_item").WillReturnRows(sqlmock.NewRows(itemCols).AddRow("i1", "WORK", "w1", "Oil", 1, "zz"))
		_, err := NewPostgres().FindGeneratedByServiceOrder(context.Background(), db, "SO-1")
		assert.Error(t, err)
	})
}

func TestPostgres_UpdateStatus(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectExec("UPDATE budget SET status").WithArgs("b1", "CANCELLED").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, NewPostgres().UpdateStatus(context.Background(), db, "b1", budget.StatusCancelled))

	mock.ExpectExec("UPDATE budget").WillReturnError(errors.New("upd boom"))
	assert.EqualError(t, NewPostgres().UpdateStatus(context.Background(), db, "b1", budget.StatusCancelled), "upd boom")
}
