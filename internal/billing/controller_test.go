package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bearer(t *testing.T, userID string, roles ...auth.Role) *http.Request {
	t.Helper()
	token, err := auth.GenerateToken(userID, roles, time.Now().Add(time.Hour))
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func withParam(key, value string) context.Context {
	return context.WithValue(context.Background(), key, value)
}

func newController(t *testing.T) (*Controller, *Service, *harness) {
	t.Helper()
	h := newHarness(t, &fakeProvider{})
	c := NewController(h.svc)
	return c, h.svc, h
}

func TestGetBudget(t *testing.T) {
	c, _, h := newController(t)
	b := h.seedBudget(budget.StatusGenerated)

	t.Run("attendant sees any budget", func(t *testing.T) {
		resp, err := c.GetBudget(withParam("serviceOrderId", "SO-1"), bearer(t, "staff", auth.ATTENDANT))
		require.NoError(t, err)
		assert.Equal(t, b.ID, resp.ID)
		assert.Equal(t, "221.00", resp.TotalAmount)
		assert.Equal(t, "GENERATED", resp.Status)
		require.Len(t, resp.Items, 2)
		assert.Equal(t, "WORK", resp.Items[0].Kind)
		assert.Equal(t, "150.00", resp.Items[0].UnitPrice)
		assert.Equal(t, "35.50", resp.Items[1].UnitPrice)
		assert.Equal(t, 2, resp.Items[1].Quantity)
	})

	t.Run("mechanic sees any budget", func(t *testing.T) {
		_, err := c.GetBudget(withParam("serviceOrderId", "SO-1"), bearer(t, "m", auth.MECHANIC))
		assert.NoError(t, err)
	})

	t.Run("owner customer sees its budget", func(t *testing.T) {
		_, err := c.GetBudget(withParam("serviceOrderId", "SO-1"), bearer(t, "cust-1", auth.CUSTOMER))
		assert.NoError(t, err)
	})

	t.Run("other customer gets not found", func(t *testing.T) {
		_, err := c.GetBudget(withParam("serviceOrderId", "SO-1"), bearer(t, "cust-2", auth.CUSTOMER))
		assert.ErrorIs(t, err, budget.ErrNotFound)
	})

	t.Run("no budget is not found", func(t *testing.T) {
		_, err := c.GetBudget(withParam("serviceOrderId", "SO-404"), bearer(t, "staff", auth.ATTENDANT))
		assert.ErrorIs(t, err, budget.ErrNotFound)
	})

	t.Run("missing token", func(t *testing.T) {
		_, err := c.GetBudget(withParam("serviceOrderId", "SO-1"), httptest.NewRequest(http.MethodGet, "/", nil))
		assert.Error(t, err)
	})
}

func TestGetPayment(t *testing.T) {
	c, _, h := newController(t)
	h.seedPayment(payment.StatusPending)

	resp, err := c.GetPayment(withParam("serviceOrderId", "SO-1"), nil)
	require.NoError(t, err)
	assert.Equal(t, "pay-1", resp.ID)
	assert.Equal(t, "221.00", resp.Amount)
	assert.Equal(t, "PENDING", resp.Status)
	assert.Equal(t, "internal", resp.Provider)

	_, err = c.GetPayment(withParam("serviceOrderId", "SO-404"), nil)
	assert.ErrorIs(t, err, payment.ErrNotFound)
}

func TestConfirmAndReject_UseRequestIDHeader(t *testing.T) {
	c, _, h := newController(t)
	h.seedPayment(payment.StatusPending)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Request-Id", "req-from-gateway")
	h.mock.ExpectBegin()
	h.outbox(1)
	h.mock.ExpectCommit()
	require.NoError(t, c.Confirm(withParam("id", "pay-1"), req))
	h.assertDB()

	h2 := newHarness(t, &fakeProvider{})
	c2 := NewController(h2.svc)
	h2.seedPayment(payment.StatusPending)
	h2.mock.ExpectBegin()
	h2.outbox(1)
	h2.mock.ExpectCommit()
	require.NoError(t, c2.Reject(withParam("id", "pay-1"), httptest.NewRequest(http.MethodPost, "/", nil)))
	h2.assertDB()
}

func TestConfirm_ErrorsAreMapped(t *testing.T) {
	c, _, h := newController(t)
	h.mock.ExpectBegin()
	h.mock.ExpectRollback()
	err := c.Confirm(withParam("id", "missing"), httptest.NewRequest(http.MethodPost, "/", nil))
	assert.True(t, errors.Is(err, payment.ErrNotFound))
}

func TestToResponses(t *testing.T) {
	now := time.Now().UTC()
	b := &budget.Budget{ID: "b", ServiceOrderID: "SO", Status: budget.StatusCancelled, TotalAmount: decimal.RequireFromString("1"), CreatedAt: now,
		Items: []budget.Item{{Kind: budget.KindSupply, RefID: "s", Name: "n", Quantity: 1, UnitPrice: decimal.RequireFromString("1")}}}
	out, err := json.Marshal(toBudgetResponse(b))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"total_amount":"1.00"`)
	assert.Contains(t, string(out), `"unit_price":"1.00"`)

	p := toPaymentResponse(&payment.Payment{ID: "p", Amount: decimal.RequireFromString("9.5"), Status: payment.StatusApproved})
	assert.Equal(t, "9.50", p.Amount)
}
