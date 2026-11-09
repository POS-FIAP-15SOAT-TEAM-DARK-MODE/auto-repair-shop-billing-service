//go:build integration

// Gateway-mode tests against real Postgres only (no SNS/SQS): they check the migration, the row
// locks and the worker queries. Env: POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD,
// POSTGRES_DB, POSTGRES_SSLMODE.
package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	budgetRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/mercadopago"
	paymentRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db/postgres"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/messaging"
)

// memGateway is a payment.Gateway that keeps the payments of the test in memory.
type memGateway struct {
	mu       sync.Mutex
	payments map[string]payment.GatewayPayment
	created  int
	delay    time.Duration // how long a CreateCheckout takes, like a remote call
}

func newMemGateway() *memGateway { return &memGateway{payments: map[string]payment.GatewayPayment{}} }

func (g *memGateway) put(p payment.GatewayPayment) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.payments[p.ID] = p
}

func (g *memGateway) CreateCheckout(_ context.Context, r payment.CheckoutRequest) (payment.Checkout, error) {
	time.Sleep(g.delay)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.created++
	return payment.Checkout{ExternalID: "pref-" + r.PaymentID, URL: "https://mp/init/" + r.PaymentID}, nil
}
func (g *memGateway) FindPayments(_ context.Context, ref string) ([]payment.GatewayPayment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []payment.GatewayPayment
	for _, p := range g.payments {
		if p.ExternalReference == ref {
			out = append(out, p)
		}
	}
	return out, nil
}
func (g *memGateway) GetPayment(_ context.Context, id string) (payment.GatewayPayment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.payments[id], nil
}
func (g *memGateway) Refund(_ context.Context, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.payments[id]
	p.Status = "refunded"
	g.payments[id] = p
	return nil
}
func (g *memGateway) CancelPayment(_ context.Context, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.payments[id]
	p.Status = "cancelled"
	g.payments[id] = p
	return nil
}
func (g *memGateway) FindCheckout(context.Context, string) (payment.Checkout, bool, error) {
	return payment.Checkout{}, false, nil
}
func (g *memGateway) ExpireCheckout(context.Context, string) error { return nil }

type gwEnv struct {
	t       *testing.T
	ctx     context.Context
	db      *sql.DB
	svc     *billing.Service
	gw      *memGateway
	router  http.Handler
	now     time.Time
	budget  *budget.Budget
	payment *payment.Payment
}

const webhookSecret = "integration-secret"

func setupGateway(t *testing.T) *gwEnv {
	t.Helper()
	db := openDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, "TRUNCATE budget, budget_item, payment, processed_message, outbox CASCADE")
	require.NoError(t, err)

	e := &gwEnv{t: t, ctx: ctx, db: db, gw: newMemGateway(), now: time.Now().UTC()}
	idGen := id.NewIDGenerator()
	e.svc = billing.NewService(db, budgetRepo.NewPostgres(), paymentRepo.NewPostgres(), payment.NewInternalProvider(payment.ModeManual), idGen.NewUUID).
		WithGateway(billing.GatewayConfig{Gateway: e.gw, NotificationURL: "https://example.test/v1/webhooks/mercadopago", CheckoutTTL: 48 * time.Hour})
	verifier := mercadopago.Verifier{Secret: webhookSecret, Tolerance: 5 * time.Minute}
	e.router = routing.SetupRouter(&container.HandlersWrapper{Billing: billing.NewController(e.svc).WithWebhook(verifier)}, &container.Middlewares{})

	// A GENERATED budget of 221.00 for the order, like GenerateBudget would leave it.
	b, err := budget.NewBudget(idGen.NewUUID, "01JD8Z4R3Q8M2V7K5N9XWABCDE", "cust-1",
		[]budget.WorkLine{{WorkID: "w1", Name: "Oil", UnitPrice: decimal.RequireFromString("150")}},
		[]budget.SupplyLine{{SupplyID: "s1", Name: "Filter", Quantity: 2, UnitPrice: decimal.RequireFromString("35.5")}})
	require.NoError(t, err)
	require.NoError(t, budgetRepo.NewPostgres().Create(ctx, db, b))
	e.budget = b
	return e
}

// newReplica is a second instance of the service on the same database, like another pod.
func (e *gwEnv) newReplica() *billing.Service {
	return billing.NewService(e.db, budgetRepo.NewPostgres(), paymentRepo.NewPostgres(), payment.NewInternalProvider(payment.ModeManual), id.NewIDGenerator().NewUUID)
}

// pass lets the reconciliation interval go by, then runs one pass of the gateway worker.
func (e *gwEnv) pass() {
	e.t.Helper()
	_, err := e.db.ExecContext(e.ctx, "UPDATE payment SET last_checked_at = last_checked_at - interval '1 minute'")
	require.NoError(e.t, err)
	e.svc.ProcessGatewayWork(e.ctx)
}

// requestPayment delivers RequestPayment the way the consumer does: inside a transaction.
func (e *gwEnv) requestPayment() *payment.Payment {
	e.t.Helper()
	tx, err := e.db.BeginTx(e.ctx, nil)
	require.NoError(e.t, err)
	env := messaging.Envelope{MessageID: "b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11", SagaID: "0f3c9a2e-6a1d-4a53-8a6e-5d3b1f2c7e90",
		Type: "RequestPayment", ServiceOrderID: e.budget.ServiceOrderID, RequestID: "req-1", Payload: []byte(`{"amount":"221.00"}`)}
	require.NoError(e.t, e.svc.RequestPayment(e.ctx, tx, env))
	require.NoError(e.t, tx.Commit())
	p, err := paymentRepo.NewPostgres().FindLatestByServiceOrder(e.ctx, e.db, e.budget.ServiceOrderID)
	require.NoError(e.t, err)
	e.payment = p
	return p
}

func (e *gwEnv) reload() *payment.Payment {
	e.t.Helper()
	p, err := paymentRepo.NewPostgres().FindByID(e.ctx, e.db, e.payment.ID)
	require.NoError(e.t, err)
	return p
}

func (e *gwEnv) outboxTypes() []string {
	e.t.Helper()
	rows, err := e.db.QueryContext(e.ctx, "SELECT type FROM outbox ORDER BY id")
	require.NoError(e.t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(e.t, rows.Scan(&s))
		out = append(out, s)
	}
	return out
}

func gwPayment(paymentID, gatewayID, status string) payment.GatewayPayment {
	return payment.GatewayPayment{ID: gatewayID, Status: status, StatusDetail: "detail", ExternalReference: paymentID, Amount: decimal.RequireFromString("221")}
}

func TestGateway_RequestPaymentAndTheWorkerOpenTheCheckoutOnPostgres(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()

	assert.Equal(t, payment.ProviderMercadoPago, p.Provider)
	assert.Equal(t, payment.StatusPending, p.Status)
	require.NotNil(t, p.ExpiresAt)
	assert.WithinDuration(t, e.now.Add(48*time.Hour), *p.ExpiresAt, time.Minute)
	assert.Empty(t, p.ExternalID)

	e.pass()
	got := e.reload()
	assert.Equal(t, "pref-"+p.ID, got.ExternalID)
	assert.Equal(t, "https://mp/init/"+p.ID, got.CheckoutURL)
	assert.Equal(t, 1, e.gw.created)

	e.pass() // nothing to open again
	assert.Equal(t, 1, e.gw.created)
	assert.Empty(t, e.outboxTypes(), "no saga event while the payment is pending")
}

func TestGateway_ReconciliationSettlesAnApprovalThenARefundCompletesTheCycle(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.pass()

	e.gw.put(gwPayment(p.ID, "1001", "approved"))
	e.pass()
	got := e.reload()
	assert.Equal(t, payment.StatusApproved, got.Status)
	assert.Equal(t, "1001", got.GatewayPaymentID)
	assert.Equal(t, []string{"PaymentApproved"}, e.outboxTypes())

	// RefundPayment: only requests the refund; PaymentRefunded waits for the gateway
	tx, err := e.db.BeginTx(e.ctx, nil)
	require.NoError(t, err)
	require.NoError(t, e.svc.RefundPayment(e.ctx, tx, messaging.Envelope{SagaID: got.SagaID, Type: "RefundPayment", ServiceOrderID: got.ServiceOrderID, RequestID: "req-2", Payload: []byte(`{}`)}))
	require.NoError(t, tx.Commit())
	assert.Equal(t, payment.StatusApproved, e.reload().Status)
	assert.NotNil(t, e.reload().RefundRequestedAt)

	e.pass()
	final := e.reload()
	assert.Equal(t, payment.StatusRefunded, final.Status)
	assert.NotNil(t, final.CheckoutClosedAt, "the checkout of a finished payment is closed")
	assert.Equal(t, []string{"PaymentApproved", "PaymentRefunded"}, e.outboxTypes())
}

func TestGateway_ExpiryOfAnInProcessPaymentRejectsItOnPostgres(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.pass()
	e.gw.put(gwPayment(p.ID, "1001", "in_process"))
	e.pass()
	assert.Equal(t, "1001", e.reload().GatewayPaymentID)
	assert.Equal(t, payment.StatusPending, e.reload().Status)

	_, err := e.db.ExecContext(e.ctx, "UPDATE payment SET expires_at = now() - interval '1 hour' WHERE id = $1", p.ID)
	require.NoError(t, err)
	e.pass()

	assert.Equal(t, payment.StatusRejected, e.reload().Status)
	assert.Equal(t, "cancelled", e.gw.payments["1001"].Status, "the gateway payment was cancelled")
	assert.Equal(t, []string{"PaymentFailed"}, e.outboxTypes())
}

func TestGateway_ConcurrentNotificationsApplyTheApprovalOnce(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.pass()

	var wg sync.WaitGroup
	results := make(chan billing.ApplyResult, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := e.svc.ApplyGatewayPayment(e.ctx, gwPayment(p.ID, "1001", "approved"))
			assert.NoError(t, err)
			results <- res
		}()
	}
	wg.Wait()
	close(results)

	applied := 0
	for r := range results {
		if r == billing.ResultApplied {
			applied++
		}
	}
	assert.Equal(t, 1, applied, "the row lock lets exactly one notification apply the approval")
	assert.Equal(t, []string{"PaymentApproved"}, e.outboxTypes(), "one event, never two")
	assert.Equal(t, payment.StatusApproved, e.reload().Status)
}

// ---- the public webhook through the router ----

func signedWebhook(secret, dataID, requestID string, at time.Time) *http.Request {
	ts := strconv.FormatInt(at.Unix(), 10) // Mercado Pago sends seconds
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("id:" + dataID + ";request-id:" + requestID + ";ts:" + ts + ";"))
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/mercadopago?data.id="+dataID+"&type=payment", nil)
	req.Header.Set("x-request-id", requestID)
	req.Header.Set("x-signature", fmt.Sprintf("ts=%s,v1=%s", ts, hex.EncodeToString(mac.Sum(nil))))
	return req
}

func TestGateway_WebhookWithAValidSignatureSettlesThePayment(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.pass()
	e.gw.put(gwPayment(p.ID, "186237542593", "approved"))

	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, signedWebhook(webhookSecret, "186237542593", "req-7", time.Now()))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "applied")
	assert.Equal(t, payment.StatusApproved, e.reload().Status)
	assert.Equal(t, []string{"PaymentApproved"}, e.outboxTypes())

	rec = httptest.NewRecorder()
	e.router.ServeHTTP(rec, signedWebhook(webhookSecret, "186237542593", "req-8", time.Now()))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "duplicate", "the same notification again changes nothing")
	assert.Len(t, e.outboxTypes(), 1)
}

func TestGateway_WebhookWithAWrongOrStaleSignatureIsRefused(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.pass()
	e.gw.put(gwPayment(p.ID, "186237542593", "approved"))

	for name, req := range map[string]*http.Request{
		"wrong secret":    signedWebhook("wrong", "186237542593", "req-1", time.Now()),
		"stale timestamp": signedWebhook(webhookSecret, "186237542593", "req-1", time.Now().Add(-time.Hour)),
	} {
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
	}
	assert.Equal(t, payment.StatusPending, e.reload().Status)
	assert.Empty(t, e.outboxTypes())
}

func TestGateway_ManualConfirmIsRefusedOnceTheCheckoutExists(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.pass()
	assert.ErrorIs(t, e.svc.Confirm(e.ctx, p.ID, "req-1"), payment.ErrGatewayManaged)
	assert.Equal(t, payment.StatusPending, e.reload().Status)
}

func TestGateway_ListWorkServesTheLeastRecentlyCheckedFirstOnPostgres(t *testing.T) {
	e := setupGateway(t)
	repo := paymentRepo.NewPostgres()
	for _, so := range []string{"A", "B", "C"} {
		_, err := e.db.ExecContext(e.ctx, `INSERT INTO payment (id, budget_id, service_order_id, saga_id, amount, status, provider, external_id)
			VALUES ($1, $2, $3, $4, 10, 'PENDING', 'mercadopago', $5)`, "pay-"+so, e.budget.ID, "SO-"+so, "saga-"+so, "pref-"+so)
		require.NoError(t, err)
	}
	ids := func(now time.Time) []string {
		list, err := repo.ListWork(e.ctx, e.db, payment.WorkReconcile, now, 2)
		require.NoError(t, err)
		var out []string
		for _, p := range list {
			out = append(out, p.ID)
		}
		return out
	}
	now := time.Now().UTC()
	assert.Equal(t, []string{"pay-A", "pay-B"}, ids(now), "never checked: creation order")

	require.NoError(t, repo.MarkChecked(e.ctx, e.db, "pay-A", now))
	require.NoError(t, repo.MarkChecked(e.ctx, e.db, "pay-B", now))
	assert.Equal(t, []string{"pay-C"}, ids(now), "the ones just checked wait for the reconcile interval")

	later := now.Add(payment.ReconcileEvery + time.Second)
	assert.Equal(t, []string{"pay-C", "pay-A"}, ids(later), "never checked first, then the oldest check")
}

func TestGateway_PendingStatsOnPostgres(t *testing.T) {
	e := setupGateway(t)
	repo := paymentRepo.NewPostgres()
	none, err := repo.PendingStats(e.ctx, e.db)
	require.NoError(t, err)
	assert.Zero(t, none.Count)
	assert.Nil(t, none.Oldest)

	e.requestPayment()
	got, err := repo.PendingStats(e.ctx, e.db)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Count)
	require.NotNil(t, got.Oldest)
	assert.WithinDuration(t, time.Now(), *got.Oldest, time.Minute)
}

func TestGateway_TwoReplicasOpenTheCheckoutOnce(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	e.gw.delay = 200 * time.Millisecond // long enough for the replicas to overlap if nothing keeps them apart
	other := e.newReplica()
	lockA := workLock{postgres.NewAdvisoryLock(e.db, 4242)}
	lockB := workLock{postgres.NewAdvisoryLock(e.db, 4242)}
	e.svc.WithGateway(billing.GatewayConfig{Gateway: e.gw, CheckoutTTL: 48 * time.Hour, WorkLock: lockA})
	other.WithGateway(billing.GatewayConfig{Gateway: e.gw, CheckoutTTL: 48 * time.Hour, WorkLock: lockB})

	var wg sync.WaitGroup
	for _, svc := range []*billing.Service{e.svc, other, e.svc, other, e.svc, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.ProcessGatewayWork(e.ctx)
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, e.gw.created, "the replicas take turns: the checkout of %s is opened once", p.ID)
}

func TestGateway_AdvisoryLockExcludesAnotherSessionUntilReleased(t *testing.T) {
	e := setupGateway(t)
	first := postgres.NewAdvisoryLock(e.db, 4243)
	second := postgres.NewAdvisoryLock(e.db, 4243)

	lease, ok, err := first.TryLock(e.ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, lease.Held(e.ctx))

	_, ok, err = second.TryLock(e.ctx)
	require.NoError(t, err)
	assert.False(t, ok, "held by the first session")

	lease.Release()
	lease2, ok, err := second.TryLock(e.ctx)
	require.NoError(t, err)
	assert.True(t, ok, "free again after the release")
	lease2.Release()
}

func TestGateway_AdvisoryLockIsLostWithItsSessionAndTheLeaseSaysSo(t *testing.T) {
	e := setupGateway(t)
	lock := postgres.NewAdvisoryLock(e.db, 4244)
	lease, ok, err := lock.TryLock(e.ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// the server drops the session that holds the lock (a restart, a failover, an admin)
	_, err = e.db.ExecContext(e.ctx, "SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype = 'advisory' AND objid = 4244 AND granted")
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return !lease.Held(e.ctx) }, 5*time.Second, 50*time.Millisecond, "a dead session means the lock is gone")
	lease.Release() // must not panic or hang on a dead connection

	again, ok, err := lock.TryLock(e.ctx)
	require.NoError(t, err)
	assert.True(t, ok, "another replica can take over")
	again.Release()
}

// workLock makes the Postgres advisory lock a billing.WorkLock, like the bootstrap does.
type workLock struct{ lock *postgres.AdvisoryLock }

func (w workLock) TryLock(ctx context.Context) (billing.WorkLease, bool, error) {
	lease, ok, err := w.lock.TryLock(ctx)
	if err != nil || !ok {
		return nil, false, err
	}
	return lease, true, nil
}

func TestGateway_RefundBacklogOnPostgres(t *testing.T) {
	e := setupGateway(t)
	repo := paymentRepo.NewPostgres()
	p := e.requestPayment()

	none, err := repo.PendingStats(e.ctx, e.db)
	require.NoError(t, err)
	assert.Equal(t, 1, none.Count, "the payment is pending")
	assert.Zero(t, none.RefundsPending)
	assert.Nil(t, none.OldestRefundRequest)

	_, err = e.db.ExecContext(e.ctx, "UPDATE payment SET status = 'APPROVED', gateway_payment_id = '1001', refund_requested_at = now() - interval '20 minutes' WHERE id = $1", p.ID)
	require.NoError(t, err)
	got, err := repo.PendingStats(e.ctx, e.db)
	require.NoError(t, err)
	assert.Zero(t, got.Count, "approved is no longer pending")
	assert.Equal(t, 1, got.RefundsPending)
	require.NotNil(t, got.OldestRefundRequest)
	assert.WithinDuration(t, time.Now().Add(-20*time.Minute), *got.OldestRefundRequest, time.Minute)
}

func TestGateway_UnsettledApprovalIsRecordedOnceAndRefundedOnPostgres(t *testing.T) {
	e := setupGateway(t)
	p := e.requestPayment()
	_, err := e.db.ExecContext(e.ctx, "UPDATE payment SET status = 'CANCELLED', external_id = 'pref-1', checkout_closed_at = now() WHERE id = $1", p.ID)
	require.NoError(t, err)

	// the customer pays the link of a payment that was cancelled; the notification arrives three times
	approved := gwPayment(p.ID, "1001", "approved")
	for range 3 {
		res, err := e.svc.ApplyGatewayPayment(e.ctx, approved)
		require.NoError(t, err)
		assert.Equal(t, billing.ResultRefundScheduled, res)
	}
	var rows int
	require.NoError(t, e.db.QueryRowContext(e.ctx, "SELECT count(*) FROM unsettled_payment WHERE gateway_payment_id = '1001' AND status = 'PENDING' AND reason = 'LATE_APPROVAL'").Scan(&rows))
	assert.Equal(t, 1, rows, "recorded once, however many notifications arrive")
	assert.Empty(t, e.outboxTypes(), "no event for the saga: it has moved on")

	e.gw.put(approved)
	e.pass() // asks for the refund, which the in-memory gateway applies at once
	e.pass()

	var status string
	var refundedAt sql.NullTime
	require.NoError(t, e.db.QueryRowContext(e.ctx, "SELECT status, refunded_at FROM unsettled_payment WHERE gateway_payment_id = '1001'").Scan(&status, &refundedAt))
	assert.Equal(t, "REFUNDED", status)
	assert.True(t, refundedAt.Valid)
	assert.Equal(t, payment.StatusCancelled, e.reload().Status, "the payment itself is untouched")

	stats, err := paymentRepo.NewPostgres().PendingStats(e.ctx, e.db)
	require.NoError(t, err)
	assert.Zero(t, stats.UnsettledPending)
}
