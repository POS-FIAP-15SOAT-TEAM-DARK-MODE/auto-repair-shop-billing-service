package mercadopago

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mercadopago/sdk-go/pkg/config"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
)

// fakeRequester replaces the SDK transport: it records every request and answers from a table.
type fakeRequester struct {
	calls   []recorded
	answers map[string]answer // key: "METHOD /path"
	netErr  error
}

type recorded struct {
	method, path, query string
	header              http.Header
	body                map[string]any
}

type answer struct {
	status int
	body   string
}

func (f *fakeRequester) Do(req *http.Request) (*http.Response, error) {
	rec := recorded{method: req.Method, path: req.URL.Path, query: req.URL.RawQuery, header: req.Header.Clone()}
	if req.Body != nil {
		if raw, _ := io.ReadAll(req.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.body)
		}
	}
	f.calls = append(f.calls, rec)
	if f.netErr != nil {
		return nil, f.netErr
	}
	a, ok := f.answers[req.Method+" "+req.URL.Path]
	if !ok {
		a = answer{status: 404, body: `{"message":"no fake answer"}`}
	}
	return &http.Response{
		StatusCode: a.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(a.body)),
		Request:    req,
	}, nil
}

func newGateway(t *testing.T, answers map[string]answer) (*Gateway, *fakeRequester) {
	t.Helper()
	if _, ok := answers["GET /checkout/preferences/search"]; !ok {
		if answers == nil {
			answers = map[string]answer{}
		}
		answers["GET /checkout/preferences/search"] = answer{200, `{"elements":[],"total":0}`} // no open preference yet
	}
	fr := &fakeRequester{answers: answers}
	g, err := New("TEST-token", config.WithHTTPClient(fr))
	require.NoError(t, err)
	return g, fr
}

func TestCreateCheckout_SendsTheExpectedPreference(t *testing.T) {
	g, fr := newGateway(t, map[string]answer{
		"POST /checkout/preferences": {201, `{"id":"pref-1","init_point":"https://mp/init","sandbox_init_point":"https://mp/sandbox"}`},
	})
	expires := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	got, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{
		PaymentID: "pay-1", Title: "Ordem de serviço 01JD", Amount: decimal.RequireFromString("75.56"),
		NotificationURL: "https://example.test/v1/webhooks/mercadopago", ExpiresAt: expires,
	})
	require.NoError(t, err)
	assert.Equal(t, payment.Checkout{ExternalID: "pref-1", URL: "https://mp/init"}, got, "init_point is the only URL used")

	call := fr.calls[len(fr.calls)-1]
	assert.Equal(t, "Bearer TEST-token", call.header.Get("Authorization"))
	assert.Equal(t, "checkout-pay-1", call.header.Get("X-Idempotency-Key"))
	assert.Equal(t, "pay-1", call.body["external_reference"])
	assert.Equal(t, "https://example.test/v1/webhooks/mercadopago", call.body["notification_url"])
	assert.Equal(t, true, call.body["expires"])
	assert.True(t, strings.HasPrefix(call.body["expiration_date_to"].(string), "2026-11-04T12:00:00"))
	item := call.body["items"].([]any)[0].(map[string]any)
	assert.Equal(t, 75.56, item["unit_price"])
	assert.Equal(t, "BRL", item["currency_id"])
	assert.Equal(t, float64(1), item["quantity"])
	assert.Equal(t, "Ordem de serviço 01JD", item["title"])
}

func TestCreateCheckout_OmitsAnEmptyNotificationURL(t *testing.T) {
	g, fr := newGateway(t, map[string]answer{"POST /checkout/preferences": {201, `{"id":"p","init_point":"u"}`}})
	_, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "x", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now()})
	require.NoError(t, err)
	_, present := fr.calls[len(fr.calls)-1].body["notification_url"]
	assert.False(t, present)
}

func TestErrorsAreClassifiedAsTransientOrPermanent(t *testing.T) {
	create := func(g *Gateway) error {
		_, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "x", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now()})
		return err
	}
	for status, transient := range map[int]bool{400: false, 404: false, 422: false, 401: true, 403: true, 408: true, 409: true, 429: true, 500: true, 503: true} {
		g, _ := newGateway(t, map[string]answer{"POST /checkout/preferences": {status, `{"message":"nope","status":0}`}})
		err := create(g)
		require.Error(t, err, status)
		assert.Equal(t, transient, payment.IsTransient(err), "status %d", status)
	}

	// no answer at all (network failure): transient
	g, fr := newGateway(t, nil)
	fr.netErr = errors.New("connection reset")
	err := create(g)
	require.Error(t, err)
	assert.True(t, payment.IsTransient(err))
}

func TestFindPayments_FiltersByExternalReference(t *testing.T) {
	g, fr := newGateway(t, map[string]answer{
		"GET /v1/payments/search": {200, `{"paging":{"total":2},"results":[
			{"id":11,"status":"rejected","status_detail":"cc_rejected_other_reason","external_reference":"pay-1","transaction_amount":75.56},
			{"id":12,"status":"approved","status_detail":"accredited","external_reference":"pay-1","transaction_amount":75.56}]}`},
	})
	got, err := g.FindPayments(context.Background(), "pay-1")
	require.NoError(t, err)
	assert.Contains(t, fr.calls[0].query, "external_reference=pay-1")
	require.Len(t, got, 2)
	assert.Equal(t, "12", got[1].ID)
	assert.Equal(t, "approved", got[1].Status)
	assert.Equal(t, "accredited", got[1].StatusDetail)
	assert.Equal(t, "pay-1", got[1].ExternalReference)
	assert.True(t, got[1].Amount.Equal(decimal.RequireFromString("75.56")))

	g, _ = newGateway(t, map[string]answer{"GET /v1/payments/search": {500, `{"message":"down"}`}})
	_, err = g.FindPayments(context.Background(), "pay-1")
	assert.True(t, payment.IsTransient(err))
}

func TestGetPayment(t *testing.T) {
	g, _ := newGateway(t, map[string]answer{"GET /v1/payments/186237542593": {200, `{"id":186237542593,"status":"approved","external_reference":"pay-1","transaction_amount":10}`}})
	got, err := g.GetPayment(context.Background(), "186237542593")
	require.NoError(t, err)
	assert.Equal(t, "186237542593", got.ID)
	assert.Equal(t, "approved", got.Status)
	assert.True(t, got.Amount.Equal(decimal.NewFromInt(10)))

	_, err = g.GetPayment(context.Background(), "not-a-number")
	require.Error(t, err)
	assert.False(t, payment.IsTransient(err), "a malformed id is permanent")

	g, _ = newGateway(t, map[string]answer{"GET /v1/payments/9": {404, `{"message":"payment not found"}`}})
	_, err = g.GetPayment(context.Background(), "9")
	require.Error(t, err)
	assert.False(t, payment.IsTransient(err))
}

func TestRefund_UsesAnIdempotencyKey(t *testing.T) {
	g, fr := newGateway(t, map[string]answer{"POST /v1/payments/12/refunds": {201, `{"id":5,"payment_id":12,"amount":10,"status":"approved"}`}})
	require.NoError(t, g.Refund(context.Background(), "12"))
	assert.Equal(t, "refund-12", fr.calls[0].header.Get("X-Idempotency-Key"))

	g, _ = newGateway(t, map[string]answer{"POST /v1/payments/12/refunds": {401, `{"message":"Unauthorized use of live credentials","error":"unauthorized","status":401}`}})
	err := g.Refund(context.Background(), "12")
	require.Error(t, err)
	assert.True(t, payment.IsTransient(err), "the 401 seen in the sandbox is retried: it can be a token that gets fixed")

	assert.Error(t, g.Refund(context.Background(), "x"))
}

func TestCancelPayment(t *testing.T) {
	g, fr := newGateway(t, map[string]answer{"PUT /v1/payments/12": {200, `{"id":12,"status":"cancelled"}`}})
	require.NoError(t, g.CancelPayment(context.Background(), "12"))
	assert.Equal(t, "cancelled", fr.calls[0].body["status"])

	assert.Error(t, g.CancelPayment(context.Background(), "x"))
	g, _ = newGateway(t, map[string]answer{"PUT /v1/payments/12": {400, `{"message":"cannot cancel an approved payment"}`}})
	assert.Error(t, g.CancelPayment(context.Background(), "12"))
}

func TestExpireCheckout_SendsAPastExpiration(t *testing.T) {
	g, fr := newGateway(t, map[string]answer{"PUT /checkout/preferences/pref-1": {200, `{"id":"pref-1"}`}})
	require.NoError(t, g.ExpireCheckout(context.Background(), "pref-1"))
	call := fr.calls[0]
	assert.Equal(t, true, call.body["expires"])
	at, err := time.Parse(time.RFC3339, call.body["expiration_date_to"].(string))
	require.NoError(t, err)
	assert.True(t, at.Before(time.Now()))

	g, _ = newGateway(t, map[string]answer{"PUT /checkout/preferences/pref-1": {500, `{"message":"down"}`}})
	assert.True(t, payment.IsTransient(g.ExpireCheckout(context.Background(), "pref-1")))
}

func TestNew_RejectsAnInvalidConfig(t *testing.T) {
	_, err := New("TEST-token", config.WithHTTPClient(nil))
	assert.Error(t, err)
}

func TestCreateCheckout_ReusesAnOpenPreferenceOfThePayment(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	g, fr := newGateway(t, map[string]answer{
		"GET /checkout/preferences/search": {200, `{"total":2,"elements":[
			{"id":"pref-old","external_reference":"pay-1","date_created":"2026-11-02T10:00:00Z","expiration_date_to":"` + future + `"},
			{"id":"pref-new","external_reference":"pay-1","date_created":"2026-11-02T11:00:00Z","expiration_date_to":"` + future + `"}]}`},
		"GET /checkout/preferences/pref-new": {200, `{"id":"pref-new","init_point":"https://mp/init-new"}`},
	})

	co, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "pay-1", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now()})

	require.NoError(t, err)
	assert.Equal(t, payment.Checkout{ExternalID: "pref-new", URL: "https://mp/init-new"}, co)
	for _, c := range fr.calls {
		assert.NotEqual(t, "POST", c.method, "no second preference is created")
	}
	assert.Equal(t, "pay-1", queryOf(fr.calls[0])["external_reference"])
}

func TestCreateCheckout_DoesNotReuseAnExpiredOrForeignPreference(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	g, fr := newGateway(t, map[string]answer{
		"GET /checkout/preferences/search": {200, `{"total":2,"elements":[
			{"id":"closed","external_reference":"pay-1","expiration_date_to":"` + past + `"},
			{"id":"other","external_reference":"pay-2","expiration_date_to":"` + future + `"}]}`},
		"POST /checkout/preferences": {201, `{"id":"pref-1","init_point":"https://mp/init"}`},
	})

	co, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "pay-1", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now().Add(time.Hour)})

	require.NoError(t, err)
	assert.Equal(t, "pref-1", co.ExternalID)
	assert.Equal(t, "POST", fr.calls[len(fr.calls)-1].method)
}

func TestCreateCheckout_SearchFailuresAreReportedAndInvalidTokenIsRetried(t *testing.T) {
	g, _ := newGateway(t, map[string]answer{"GET /checkout/preferences/search": {500, `{"message":"down"}`}})
	_, err := g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "pay-1", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now()})
	require.Error(t, err)
	assert.True(t, payment.IsTransient(err))

	g, _ = newGateway(t, map[string]answer{"GET /checkout/preferences/search": {400, `{"message":"invalid_token","error":"bad_request","status":400}`}})
	_, err = g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "pay-1", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now()})
	require.Error(t, err)
	assert.True(t, payment.IsTransient(err), "a rejected token is a configuration problem: retried, not fatal")

	g, _ = newGateway(t, map[string]answer{"GET /checkout/preferences/search": {200, `{"total":1,"elements":[{"id":"p","external_reference":"pay-1","expiration_date_to":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}]}`},
		"GET /checkout/preferences/p": {500, `{"message":"down"}`}})
	_, err = g.CreateCheckout(context.Background(), payment.CheckoutRequest{PaymentID: "pay-1", Title: "t", Amount: decimal.NewFromInt(1), ExpiresAt: time.Now()})
	require.Error(t, err)
	assert.True(t, payment.IsTransient(err))
}

func queryOf(c recorded) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(c.query, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}
