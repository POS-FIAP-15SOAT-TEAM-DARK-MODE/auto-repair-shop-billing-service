package billing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/web"
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

// ---- webhook ----

type fakeVerifier struct {
	err       error
	gotSig    string
	gotReq    string
	gotDataID string
}

func (f *fakeVerifier) Verify(sig, reqID, dataID string) error {
	f.gotSig, f.gotReq, f.gotDataID = sig, reqID, dataID
	return f.err
}

func webhookRequest(query, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/mercadopago?"+query, strings.NewReader(body))
	req.Header.Set("x-signature", "ts=1,v1=abc")
	req.Header.Set("x-request-id", "req-9")
	return req
}

func newWebhookController(t *testing.T, v *fakeVerifier) (*Controller, *gatewayHarness) {
	t.Helper()
	g := newGatewayHarness(t)
	return NewController(g.svc).WithWebhook(v), g
}

func TestWebhook_AppliesThePaymentReadFromTheGateway(t *testing.T) {
	v := &fakeVerifier{}
	c, g := newWebhookController(t, v)
	g.seedGatewayPayment(withCheckout)
	g.gw.byID["186237542593"] = gp("186237542593", "approved", "221")
	g.tx(1)

	resp, err := c.Webhook(context.Background(), webhookRequest("data.id=186237542593&type=payment", `{"type":"payment","data":{"id":"186237542593"}}`))
	require.NoError(t, err)
	assert.Equal(t, "applied", resp.Result)
	assert.Equal(t, payment.StatusApproved, g.reload().Status)
	assert.Equal(t, "186237542593", v.gotDataID, "the signed id is the one in the query string")
	assert.Equal(t, "ts=1,v1=abc", v.gotSig)
	assert.Equal(t, "req-9", v.gotReq)
	g.assertDB()
}

func TestWebhook_FallsBackToTheIdInTheBodyAndTheTypeInTheBody(t *testing.T) {
	v := &fakeVerifier{}
	c, g := newWebhookController(t, v)
	g.seedGatewayPayment(withCheckout)
	g.gw.byID["77"] = gp("77", "in_process", "221")
	g.tx(0)
	resp, err := c.Webhook(context.Background(), webhookRequest("", `{"type":"payment","data":{"id":77}}`))
	require.NoError(t, err)
	assert.Equal(t, "duplicate", resp.Result)
	assert.Equal(t, "77", v.gotDataID)
}

func TestWebhook_RejectsABadSignatureBeforeDoingAnything(t *testing.T) {
	v := &fakeVerifier{err: errors.New("SignatureMismatch")}
	c, g := newWebhookController(t, v)
	g.seedGatewayPayment(withCheckout)
	g.gw.byID["1"] = gp("1", "approved", "221")

	_, err := c.Webhook(context.Background(), webhookRequest("data.id=1&type=payment", `{}`))
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
	assert.Equal(t, payment.StatusPending, g.reload().Status)
	g.assertDB() // no transaction was opened
}

func TestWebhook_IgnoresOtherTypesAndRejectsAnInvalidId(t *testing.T) {
	c, g := newWebhookController(t, &fakeVerifier{})
	resp, err := c.Webhook(context.Background(), webhookRequest("data.id=5&type=merchant_order", `{}`))
	require.NoError(t, err)
	assert.Equal(t, "ignored", resp.Result)

	for _, q := range []string{"type=payment", "data.id=abc&type=payment", "data.id=-5&type=payment"} {
		_, err = c.Webhook(context.Background(), webhookRequest(q, `{}`))
		assert.ErrorIs(t, err, payment.ErrInvalidNotification, q)
	}
	g.assertDB()
}

func TestWebhook_GatewayFailuresAreReturnedSoTheGatewayRetries(t *testing.T) {
	c, g := newWebhookController(t, &fakeVerifier{})
	g.gw.getErr = transient
	_, err := c.Webhook(context.Background(), webhookRequest("data.id=1&type=payment", `{}`))
	assert.ErrorIs(t, err, transient)
	status, _ := web.Error(err)
	assert.Equal(t, http.StatusInternalServerError, status)
}

func TestWebhook_UnreadableBody(t *testing.T) {
	c, _ := newWebhookController(t, &fakeVerifier{})
	req := webhookRequest("data.id=1&type=payment", `{}`)
	req.Body = io.NopCloser(errReader{})
	_, err := c.Webhook(context.Background(), req)
	assert.ErrorIs(t, err, payment.ErrInvalidNotification)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read boom") }

func TestWebhookEnabledOnlyWithAVerifierAndAGateway(t *testing.T) {
	h := newHarness(t, &fakeProvider{})
	assert.False(t, NewController(h.svc).WebhookEnabled(), "internal modes have no webhook")
	assert.False(t, NewController(h.svc).WithWebhook(&fakeVerifier{}).WebhookEnabled(), "a verifier without a gateway")
	g := newGatewayHarness(t)
	assert.False(t, NewController(g.svc).WebhookEnabled(), "a gateway without a verifier")
	assert.True(t, NewController(g.svc).WithWebhook(&fakeVerifier{}).WebhookEnabled())
}

func TestHandleGatewayNotificationWithoutAGateway(t *testing.T) {
	h := newHarness(t, &fakeProvider{})
	_, err := h.svc.HandleGatewayNotification(context.Background(), "1")
	assert.Error(t, err)
}

func TestToPaymentResponse_CarriesTheCheckoutOfAGatewayPayment(t *testing.T) {
	exp := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	got := toPaymentResponse(&payment.Payment{ID: "p", Amount: decimal.RequireFromString("10"), Status: payment.StatusPending,
		Provider: payment.ProviderMercadoPago, CheckoutURL: "https://mp/init", ExpiresAt: &exp})
	assert.Equal(t, "https://mp/init", got.CheckoutURL)
	require.NotNil(t, got.ExpiresAt)
	raw, err := json.Marshal(toPaymentResponse(&payment.Payment{ID: "p", Amount: decimal.RequireFromString("10")}))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "checkout_url", "internal payments keep the response they had")
}
