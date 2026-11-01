package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	budgetRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	paymentRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	router   http.Handler
	mock     sqlmock.Sqlmock
	budgets  budget.Repository
	payments payment.Repository
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	budgets := budgetRepo.NewInMemory()
	payments := paymentRepo.NewInMemory()
	svc := billing.NewService(db, budgets, payments, payment.NewInternalProvider(payment.ModeManual), func() string { return "id" })
	router := SetupRouter(&container.HandlersWrapper{Billing: billing.NewController(svc)}, &container.Middlewares{})
	return &fixture{router: router, mock: mock, budgets: budgets, payments: payments}
}

func token(t *testing.T, userID string, roles ...auth.Role) string {
	t.Helper()
	tok, err := auth.GenerateToken(userID, roles, time.Now().Add(time.Hour))
	require.NoError(t, err)
	return tok
}

func do(router http.Handler, method, path, bearerToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRouting_PublicAndSwagger(t *testing.T) {
	f := newFixture(t)
	assert.Equal(t, http.StatusOK, do(f.router, http.MethodGet, "/ping", "").Code)
	assert.Equal(t, http.StatusOK, do(f.router, http.MethodGet, "/swagger.yaml", "").Code)
	assert.Equal(t, http.StatusOK, do(f.router, http.MethodGet, "/metrics", "").Code)
}

func TestRouting_GetBudget(t *testing.T) {
	f := newFixture(t)
	b, err := budget.NewBudget(func() string { return "id" }, "SO-1", "cust-1",
		[]budget.WorkLine{{WorkID: "w", Name: "Oil", UnitPrice: decimal.RequireFromString("150")}}, nil)
	require.NoError(t, err)
	require.NoError(t, f.budgets.Create(context.Background(), nil, b))

	assert.Equal(t, http.StatusUnauthorized, do(f.router, http.MethodGet, "/v1/budgets/SO-1", "").Code)
	assert.Equal(t, http.StatusForbidden, do(f.router, http.MethodGet, "/v1/budgets/SO-1", token(t, "x", auth.Role("NOBODY"))).Code)

	rec := do(f.router, http.MethodGet, "/v1/budgets/SO-1", token(t, "m", auth.MECHANIC))
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "150.00", body["total_amount"])

	assert.Equal(t, http.StatusNotFound, do(f.router, http.MethodGet, "/v1/budgets/SO-404", token(t, "staff", auth.ATTENDANT)).Code)
	assert.Equal(t, http.StatusNotFound, do(f.router, http.MethodGet, "/v1/budgets/SO-1", token(t, "other", auth.CUSTOMER)).Code)
}

func TestRouting_Payments(t *testing.T) {
	f := newFixture(t)
	p := &payment.Payment{ID: "p1", BudgetID: "b", ServiceOrderID: "SO-1", Amount: decimal.RequireFromString("10"), Status: payment.StatusPending, Provider: "internal"}
	require.NoError(t, f.payments.Create(context.Background(), nil, p))

	rec := do(f.router, http.MethodGet, "/v1/payments/SO-1", token(t, "staff", auth.ATTENDANT))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"amount":"10.00"`)
	assert.Equal(t, http.StatusForbidden, do(f.router, http.MethodGet, "/v1/payments/SO-1", token(t, "c", auth.CUSTOMER)).Code)

	// confirm: transaction, payment found, transition, outbox event, commit
	f.mock.ExpectBegin()
	f.mock.ExpectExec("INSERT INTO outbox").WillReturnResult(sqlmock.NewResult(1, 1))
	f.mock.ExpectCommit()
	assert.Equal(t, http.StatusNoContent, do(f.router, http.MethodPost, "/v1/payments/p1/confirm", token(t, "staff", auth.ATTENDANT)).Code)

	// already approved: reject is a conflict
	f.mock.ExpectBegin()
	f.mock.ExpectRollback()
	assert.Equal(t, http.StatusConflict, do(f.router, http.MethodPost, "/v1/payments/p1/reject", token(t, "staff", auth.ATTENDANT)).Code)

	assert.Equal(t, http.StatusForbidden, do(f.router, http.MethodPost, "/v1/payments/p1/confirm", token(t, "c", auth.CUSTOMER)).Code)
	assert.Equal(t, http.StatusNotFound, do(f.router, http.MethodGet, "/v1/payments/SO-404", token(t, "staff", auth.ATTENDANT)).Code)
}
