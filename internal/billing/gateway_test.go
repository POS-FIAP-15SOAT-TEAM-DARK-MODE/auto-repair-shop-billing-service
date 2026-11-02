package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakeGateway is a payment.Gateway that records its calls and answers from fields the tests set.
type fakeGateway struct {
	checkout  payment.Checkout
	createErr error
	created   []payment.CheckoutRequest

	found []payment.GatewayPayment
	finds int

	openCheckout    *payment.Checkout // what FindCheckout answers; nil means none
	findCheckoutErr error
	findCheckouts   int
	findErr         error
	byID            map[string]payment.GatewayPayment
	getErr          error

	refundErr error
	refunded  []string
	cancelErr error
	cancelled []string
	expireErr error
	expired   []string
}

func (f *fakeGateway) CreateCheckout(_ context.Context, r payment.CheckoutRequest) (payment.Checkout, error) {
	f.created = append(f.created, r)
	return f.checkout, f.createErr
}
func (f *fakeGateway) FindPayments(context.Context, string) ([]payment.GatewayPayment, error) {
	f.finds++
	return append([]payment.GatewayPayment(nil), f.found...), f.findErr
}
func (f *fakeGateway) FindCheckout(context.Context, string) (payment.Checkout, bool, error) {
	f.findCheckouts++
	if f.openCheckout == nil {
		return payment.Checkout{}, false, f.findCheckoutErr
	}
	return *f.openCheckout, true, f.findCheckoutErr
}
func (f *fakeGateway) GetPayment(_ context.Context, id string) (payment.GatewayPayment, error) {
	return f.byID[id], f.getErr
}
func (f *fakeGateway) Refund(_ context.Context, id string) error {
	f.refunded = append(f.refunded, id)
	return f.refundErr
}
func (f *fakeGateway) CancelPayment(_ context.Context, id string) error {
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}
func (f *fakeGateway) ExpireCheckout(_ context.Context, id string) error {
	f.expired = append(f.expired, id)
	return f.expireErr
}

var (
	t0        = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	transient = &payment.GatewayError{Transient: true, Err: errors.New("gateway down")}
	permanent = &payment.GatewayError{Transient: false, Err: errors.New("refused")}
)

type gatewayHarness struct {
	*harness
	gw  *fakeGateway
	now time.Time
}

func newGatewayHarness(t *testing.T) *gatewayHarness {
	t.Helper()
	h := newHarness(t, &fakeProvider{})
	gw := &fakeGateway{checkout: payment.Checkout{ExternalID: "pref-1", URL: "https://mp/init"}, byID: map[string]payment.GatewayPayment{}}
	g := &gatewayHarness{harness: h, gw: gw, now: t0}
	h.svc.WithGateway(GatewayConfig{Gateway: gw, NotificationURL: "https://example.test/v1/webhooks/mercadopago", CheckoutTTL: 48 * time.Hour})
	h.svc.now = func() time.Time { return g.now }
	return g
}

// tx expects one short transaction with n outbox rows, like the worker and the webhook open.
func (g *gatewayHarness) tx(outbox int) {
	g.mock.ExpectBegin()
	g.outbox(outbox)
	g.mock.ExpectCommit()
}

// seedGatewayPayment stores a mercadopago payment for SO-1.
func (g *gatewayHarness) seedGatewayPayment(mut func(*payment.Payment)) *payment.Payment {
	g.t.Helper()
	exp := t0.Add(48 * time.Hour)
	p := &payment.Payment{ID: "pay-1", BudgetID: "b", ServiceOrderID: "SO-1", SagaID: "saga-1",
		Amount: decimal.RequireFromString("221"), Status: payment.StatusPending, Provider: payment.ProviderMercadoPago, ExpiresAt: &exp}
	if mut != nil {
		mut(p)
	}
	require.NoError(g.t, g.payments.Create(context.Background(), nil, p))
	return p
}

func (g *gatewayHarness) reload() *payment.Payment {
	g.t.Helper()
	p, err := g.payments.FindByID(context.Background(), nil, "pay-1")
	require.NoError(g.t, err)
	return p
}

func gp(id, status string, amount string) payment.GatewayPayment {
	return payment.GatewayPayment{ID: id, Status: status, StatusDetail: "detail", ExternalReference: "pay-1", Amount: decimal.RequireFromString(amount)}
}

func withCheckout(p *payment.Payment) { p.ExternalID, p.CheckoutURL = "pref-1", "https://mp/init" }

// ---- RequestPayment in gateway mode ----

func TestGateway_RequestPaymentOnlyRecordsAPendingPayment(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedBudget(budget.StatusGenerated)
	g.begin()

	require.NoError(t, g.svc.RequestPayment(context.Background(), g.harness.tx, env(cmdRequestPayment, `{"amount":"221.00"}`)))

	p, err := g.payments.FindLatestByServiceOrder(context.Background(), nil, "SO-1")
	require.NoError(t, err)
	assert.Equal(t, payment.ProviderMercadoPago, p.Provider)
	assert.Equal(t, payment.StatusPending, p.Status)
	require.NotNil(t, p.ExpiresAt)
	assert.True(t, p.ExpiresAt.Equal(t0.Add(48*time.Hour)))
	assert.Empty(t, g.gw.created, "the remote call happens in the worker, not inside the command transaction")
	assert.Equal(t, 0, g.provider.refunded)
	g.assertDB()
}

// ---- the worker: create the checkout ----

func TestGateway_WorkerCreatesTheCheckout(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(nil)
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	require.Len(t, g.gw.created, 1)
	req := g.gw.created[0]
	assert.Equal(t, "pay-1", req.PaymentID)
	assert.Equal(t, "Ordem de serviço SO-1", req.Title)
	assert.True(t, req.Amount.Equal(decimal.RequireFromString("221")))
	assert.Equal(t, "https://example.test/v1/webhooks/mercadopago", req.NotificationURL)
	assert.True(t, req.ExpiresAt.Equal(t0.Add(48*time.Hour)))
	p := g.reload()
	assert.Equal(t, "pref-1", p.ExternalID)
	assert.Equal(t, "https://mp/init", p.CheckoutURL)
	g.assertDB()
}

func TestGateway_CheckoutFailureTransientIsRetriedWithBackoff(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(nil)
	g.gw.createErr = transient

	g.svc.ProcessGatewayWork(context.Background())
	g.svc.ProcessGatewayWork(context.Background()) // same instant: still backing off
	assert.Len(t, g.gw.created, 1)
	assert.Equal(t, payment.StatusPending, g.reload().Status)

	g.now = g.now.Add(10 * time.Second) // past the first 5 s backoff
	g.gw.createErr = nil
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.Len(t, g.gw.created, 2)
	assert.Equal(t, "pref-1", g.reload().ExternalID)
	g.assertDB()
}

func TestGateway_CheckoutRefusedForGoodFailsThePayment(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(nil)
	g.gw.createErr = permanent
	g.tx(1) // PaymentFailed

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusRejected, g.reload().Status)
	g.assertDB()
}

func TestGateway_AnotherReplicaAlreadyCreatedTheCheckout(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(nil)
	// the payment gets its checkout between the listing and the update
	g.gw.checkout = payment.Checkout{ExternalID: "pref-other", URL: "https://other"}
	g.mock.ExpectBegin()
	g.mock.ExpectCommit()
	require.NoError(t, g.svc.createCheckout(context.Background(), p))
	first := g.reload()
	require.Equal(t, "pref-other", first.ExternalID)

	g.mock.ExpectBegin()
	g.mock.ExpectCommit()
	g.gw.checkout = payment.Checkout{ExternalID: "pref-late", URL: "https://late"}
	require.NoError(t, g.svc.createCheckout(context.Background(), p))
	assert.Equal(t, "pref-other", g.reload().ExternalID, "the first checkout is kept")
	g.assertDB()
}

// ---- ApplyGatewayPayment ----

func TestGateway_ApplyApprovedPublishesPaymentApproved(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.tx(1)

	res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultApplied, res)
	p := g.reload()
	assert.Equal(t, payment.StatusApproved, p.Status)
	assert.Equal(t, "1001", p.GatewayPaymentID)
	g.assertDB()
}

func TestGateway_ApplyRejectedAndCancelledPublishPaymentFailed(t *testing.T) {
	for _, status := range []string{"rejected", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			g := newGatewayHarness(t)
			g.seedGatewayPayment(withCheckout)
			g.tx(1)
			res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", status, "221"))
			require.NoError(t, err)
			assert.Equal(t, ResultApplied, res)
			g.assertDB()
		})
	}
}

func TestGateway_ApplyInProcessKeepsPendingAndRemembersTheGatewayPayment(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.tx(0)

	res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "in_process", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultDuplicate, res)
	p := g.reload()
	assert.Equal(t, payment.StatusPending, p.Status)
	assert.Equal(t, "1001", p.GatewayPaymentID)

	g.tx(0) // the same notification again changes nothing
	res, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "in_process", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultDuplicate, res)
	g.assertDB()
}

func TestGateway_ApplyUnknownUnmappedAndInvalidAreIgnored(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)

	g.tx(0)
	unknown := gp("1", "approved", "221")
	unknown.ExternalReference = "nope"
	res, err := g.svc.ApplyGatewayPayment(context.Background(), unknown)
	require.NoError(t, err)
	assert.Equal(t, ResultUnknown, res)

	g.tx(0)
	res, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1", "in_mediation", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultIgnored, res)

	g.tx(0) // a refund of a payment that was never approved is not a valid transition
	res, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1", "refunded", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultIgnored, res)
	assert.Equal(t, payment.StatusPending, g.reload().Status)
	g.assertDB()
}

func TestGateway_ApprovedWithAnotherAmountIsNotSettled(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.tx(0)
	res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "200"))
	require.NoError(t, err)
	assert.Equal(t, ResultIgnored, res)
	assert.Equal(t, payment.StatusPending, g.reload().Status)
	g.assertDB()
}

func TestGateway_ApprovedAmountIgnoresInstallmentInterest(t *testing.T) {
	// transaction_amount is our price; the interest the buyer pays on installments is not part of it
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.tx(1)
	res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221.00"))
	require.NoError(t, err)
	assert.Equal(t, ResultApplied, res)
	g.assertDB()
}

func TestGateway_LateAndExtraApprovalsAreFlaggedNotApplied(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusCancelled })
	g.tx(0)
	res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultIgnored, res, "late approval on a cancelled payment")

	g2 := newGatewayHarness(t)
	g2.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status = payment.StatusApproved
		p.GatewayPaymentID = "1001"
	})
	g2.tx(0)
	res, err = g2.svc.ApplyGatewayPayment(context.Background(), gp("1002", "approved", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultIgnored, res, "a second approved payment")
	assert.Equal(t, "1001", g2.reload().GatewayPaymentID, "the first approved payment keeps settling the order")
}

func TestGateway_ApplyRefundedAfterApprovalPublishesPaymentRefunded(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status = payment.StatusApproved
		p.GatewayPaymentID = "1001"
	})
	g.tx(1)
	res, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "refunded", "221"))
	require.NoError(t, err)
	assert.Equal(t, ResultApplied, res)
	assert.Equal(t, payment.StatusRefunded, g.reload().Status)
	g.assertDB()
}

func TestGateway_ApplyReportsDatabaseErrors(t *testing.T) {
	g := newGatewayHarness(t)
	g.mock.ExpectBegin().WillReturnError(errors.New("begin boom"))
	_, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1", "approved", "221"))
	assert.EqualError(t, err, "begin boom")

	g.seedGatewayPayment(withCheckout)
	g.mock.ExpectBegin()
	g.mock.ExpectExec("INSERT INTO outbox").WillReturnError(errors.New("outbox boom"))
	g.mock.ExpectRollback()
	_, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221"))
	assert.EqualError(t, err, "outbox boom")
}

// ---- the worker: reconcile ----

func TestGateway_ReconcileFindsAnApprovedPaymentWithoutAWebhook(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.found = []payment.GatewayPayment{gp("1002", "rejected", "221"), gp("1001", "approved", "221")} // newest first
	g.tx(1)                                                                                             // only the approval is applied; the rejection finds the payment settled

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusApproved, g.reload().Status)
	assert.Equal(t, "1001", g.reload().GatewayPaymentID)
	g.assertDB()
}

func TestGateway_ReconcileGatewayFailureLeavesThePaymentAlone(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.findErr = transient
	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, payment.StatusPending, g.reload().Status)
}

// ---- the worker: expiry ----

func TestGateway_ExpiryOfAnUnpaidCheckoutCancelsAndAnswersPaymentFailed(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.now = t0.Add(49 * time.Hour)
	g.tx(1) // settle: PaymentFailed
	g.tx(0) // close the checkout

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusCancelled, g.reload().Status)
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	assert.NotNil(t, g.reload().CheckoutClosedAt)
	g.assertDB()
}

func TestGateway_ExpiryOfAnInProcessPaymentCancelsItAtTheGatewayAndRejects(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.GatewayPaymentID = "1001" })
	g.gw.found = []payment.GatewayPayment{gp("1001", "in_process", "221")}
	g.now = t0.Add(49 * time.Hour)
	g.tx(0) // reconcile (reconcile step): same state, nothing to change
	g.tx(0) // reconcile inside expire
	g.tx(1) // settle: PaymentFailed
	g.tx(0) // close the checkout

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusRejected, g.reload().Status)
	assert.Equal(t, []string{"1001"}, g.gw.cancelled)
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	g.assertDB()
}

func TestGateway_ApprovalFoundAtExpiryWins(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.GatewayPaymentID = "1001" })
	g.gw.found = []payment.GatewayPayment{gp("1001", "approved", "221")}
	g.now = t0.Add(49 * time.Hour)
	g.tx(1) // the reconcile step applies the approval; the expiry then finds nothing pending

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusApproved, g.reload().Status)
	assert.Empty(t, g.gw.cancelled)
	g.assertDB()
}

func TestGateway_ExpiryKeepsPendingWhenTheGatewayCancellationFails(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.GatewayPaymentID = "1001" })
	g.gw.found = []payment.GatewayPayment{gp("1001", "in_process", "221")}
	g.gw.cancelErr = transient
	g.now = t0.Add(49 * time.Hour)
	g.tx(0)
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusPending, g.reload().Status)
	assert.Equal(t, []string{"1001"}, g.gw.cancelled)
	g.assertDB()
}

// ---- refunds ----

func TestGateway_RefundCommandOnAnApprovedPaymentWaitsForTheGateway(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status = payment.StatusApproved
		p.GatewayPaymentID = "1001"
	})
	g.begin()

	require.NoError(t, g.svc.RefundPayment(context.Background(), g.harness.tx, env(cmdRefundPayment, `{}`)))
	p := g.reload()
	assert.Equal(t, payment.StatusApproved, p.Status, "not refunded until the gateway says so")
	assert.NotNil(t, p.RefundRequestedAt)
	assert.Equal(t, 0, g.provider.refunded)

	// a repeated command does not move the request time
	first := *p.RefundRequestedAt
	g.now = g.now.Add(time.Hour)
	g.begin()
	require.NoError(t, g.svc.RefundPayment(context.Background(), g.harness.tx, env(cmdRefundPayment, `{}`)))
	assert.True(t, g.reload().RefundRequestedAt.Equal(first))

	// the worker refunds at the gateway and settles once the gateway reports it refunded
	g.gw.byID["1001"] = gp("1001", "refunded", "221")
	g.tx(1) // PaymentRefunded
	g.tx(0) // close the checkout
	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, []string{"1001"}, g.gw.refunded)
	assert.Equal(t, payment.StatusRefunded, g.reload().Status)
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	g.assertDB()
}

func TestGateway_RefundIsSubmittedOnceAndPolledUntilConfirmed(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusApproved, "1001"
		requested := t0
		p.RefundRequestedAt = &requested
	})
	g.gw.byID["1001"] = gp("1001", "approved", "221") // not refunded yet

	g.tx(0) // apply: same status
	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, []string{"1001"}, g.gw.refunded)
	assert.Equal(t, payment.StatusApproved, g.reload().Status)

	g.now = g.now.Add(time.Minute)
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, []string{"1001"}, g.gw.refunded, "the refund is not submitted twice")
	g.assertDB()
}

func TestGateway_RefundRefusedKeepsThePaymentApproved(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusApproved, "1001"
		requested := t0
		p.RefundRequestedAt = &requested
	})
	g.gw.refundErr = permanent // the 401 seen in the sandbox

	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, payment.StatusApproved, g.reload().Status)
	assert.Equal(t, []string{"1001"}, g.gw.refunded)

	g.svc.ProcessGatewayWork(context.Background()) // backing off
	assert.Len(t, g.gw.refunded, 1)
}

func TestGateway_RefundWithoutAGatewayPaymentIdIsAnError(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(func(p *payment.Payment) { p.Status = payment.StatusApproved })
	assert.Error(t, g.svc.refund(context.Background(), p))
}

func TestGateway_RefundCommandOnAPendingPaymentCancelsItAndTheWorkerClosesTheCheckout(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.GatewayPaymentID = "1001" })
	g.begin()
	require.NoError(t, g.svc.RefundPayment(context.Background(), g.harness.tx, env(cmdRefundPayment, `{}`)))
	assert.Equal(t, payment.StatusCancelled, g.reload().Status)

	g.gw.found = nil
	g.tx(0) // close the checkout
	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, []string{"1001"}, g.gw.cancelled, "an in-process gateway payment is cancelled too")
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	g.assertDB()
}

func TestGateway_ClosingARejectedPaymentOnlyExpiresTheCheckout(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusRejected, "1001"
	})
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.Empty(t, g.gw.cancelled)
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	g.assertDB()
}

func TestGateway_ClosingRetriesTransientFailuresAndAcceptsPermanentOnes(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusRejected })
	g.gw.expireErr = transient
	g.svc.ProcessGatewayWork(context.Background())
	assert.Nil(t, g.reload().CheckoutClosedAt, "a transient failure is retried")

	g.now = g.now.Add(time.Minute)
	g.gw.expireErr = permanent // for example the checkout does not exist anymore
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.NotNil(t, g.reload().CheckoutClosedAt, "a permanent answer counts as closed")
	g.assertDB()
}

// ---- manual endpoints ----

func TestGateway_ManualConfirmAndRejectAre409ForAGatewayManagedPayment(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	for _, fn := range []func(context.Context, string, string) error{g.svc.Confirm, g.svc.Reject} {
		g.mock.ExpectBegin()
		g.mock.ExpectRollback()
		err := fn(context.Background(), "pay-1", "req-1")
		assert.ErrorIs(t, err, payment.ErrGatewayManaged)
	}
	assert.Equal(t, payment.StatusPending, g.reload().Status)
	g.assertDB()
}

// ---- configuration and helpers ----

func TestGateway_WithGatewayDefaultsAndNoOpWithoutGateway(t *testing.T) {
	h := newHarness(t, &fakeProvider{})
	h.svc.ProcessGatewayWork(context.Background()) // no gateway: nothing happens, no database call
	assert.Equal(t, payment.ProviderName, h.svc.providerName())

	h.svc.WithGateway(GatewayConfig{Gateway: &fakeGateway{}})
	assert.Equal(t, defaultCheckTTL, h.svc.checkoutTTL)
	assert.Equal(t, payment.ProviderMercadoPago, h.svc.providerName())
	h.assertDB()
}

func TestRetryStateBackoffGrowsAndIsCapped(t *testing.T) {
	var r retryState
	assert.True(t, r.ready("k", t0))
	r.fail("k", t0)
	assert.False(t, r.ready("k", t0.Add(4*time.Second)))
	assert.True(t, r.ready("k", t0.Add(5*time.Second)))
	r.fail("k", t0)
	assert.False(t, r.ready("k", t0.Add(9*time.Second)), "second failure backs off 10 s")
	for i := 0; i < 20; i++ {
		r.fail("k", t0)
	}
	assert.False(t, r.ready("k", t0.Add(maxBackoff-time.Second)))
	assert.True(t, r.ready("k", t0.Add(maxBackoff)), "the backoff never exceeds the cap")
	r.clear("k")
	assert.True(t, r.ready("k", t0))

	assert.False(t, r.refundSubmitted("p"))
	r.markRefundSubmitted("p")
	assert.True(t, r.refundSubmitted("p"))
}

func TestGateway_RunWorkerStopsWithTheContext(t *testing.T) {
	g := newGatewayHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, g.svc.RunGatewayWorker(ctx, time.Millisecond))
}

// ---- edge paths of the worker helpers ----

func TestGateway_SettleDoesNothingWhenThePaymentIsNoLongerPendingOrTheGuardSaysNo(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { p.Status = payment.StatusApproved })
	g.tx(0)
	require.NoError(t, g.svc.settle(context.Background(), "pay-1", payment.StatusRejected, nil))
	assert.Equal(t, payment.StatusApproved, g.reload().Status)

	g2 := newGatewayHarness(t)
	g2.seedGatewayPayment(withCheckout)
	g2.tx(0)
	require.NoError(t, g2.svc.settle(context.Background(), "pay-1", payment.StatusRejected, func(p *payment.Payment) bool { return p.ExternalID == "" }))
	assert.Equal(t, payment.StatusPending, g2.reload().Status)

	g2.mock.ExpectBegin()
	g2.mock.ExpectRollback()
	assert.ErrorIs(t, g2.svc.settle(context.Background(), "missing", payment.StatusRejected, nil), payment.ErrNotFound)
}

func TestGateway_ExpiryStopsWhenTheGatewayCannotBeAsked(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(withCheckout)
	g.gw.findErr = transient
	assert.Error(t, g.svc.expire(context.Background(), p), "an expiry never decides without looking at the gateway")
	assert.Equal(t, payment.StatusPending, g.reload().Status)
}

func TestGateway_ClosingAndCreatingReportDatabaseErrors(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusRejected })
	g.mock.ExpectBegin().WillReturnError(errors.New("begin boom"))
	assert.EqualError(t, g.svc.closeCheckout(context.Background(), p), "begin boom")

	g.mock.ExpectBegin().WillReturnError(errors.New("begin boom"))
	assert.EqualError(t, g.svc.createCheckout(context.Background(), g.seedGatewayPayment(func(q *payment.Payment) { q.ID = "pay-2" })), "begin boom")
}

func TestGateway_PublishOutcomeIgnoresStatusesWithoutAnEvent(t *testing.T) {
	g := newGatewayHarness(t)
	g.begin()
	assert.NoError(t, g.svc.publishOutcome(context.Background(), g.harness.tx, &payment.Payment{}, payment.StatusPending))
}

// ---- review fixes ----

func TestGateway_RefundRefusedButAlreadyDoneAtTheGatewayIsSettled(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusApproved, "1001"
		requested := t0
		p.RefundRequestedAt = &requested
	})
	g.gw.refundErr = permanent                        // the refund already exists (made from the seller's panel)
	g.gw.byID["1001"] = gp("1001", "refunded", "221") // and the payment says so
	g.tx(1)                                           // PaymentRefunded
	g.tx(0)                                           // then the checkout is closed

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusRefunded, g.reload().Status)
	assert.Empty(t, g.svc.retry.submitted, "the submitted marker is dropped once the refund is confirmed")
	g.assertDB()
}

func TestGateway_RefundRefusedAndStillApprovedReturnsTheRefusal(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusApproved, "1001"
	})
	g.gw.refundErr = permanent
	g.gw.byID["1001"] = gp("1001", "approved", "221")
	g.tx(0)
	assert.ErrorIs(t, g.svc.refund(context.Background(), p), permanent)
	g.assertDB()
}

func TestGateway_UnsettledApprovalBacksOffInsteadOfLoggingEveryPass(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.found = []payment.GatewayPayment{gp("1001", "approved", "200")} // another amount
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, 1, g.gw.finds)

	g.now = g.now.Add(time.Second) // still inside the backoff
	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, 1, g.gw.finds, "the payment waits for its backoff")
	assert.Equal(t, payment.StatusPending, g.reload().Status)
	g.assertDB()
}

func TestGateway_ALiveAttemptIsNotOverriddenByAnOlderRefusal(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	// the card was refused, then the customer tried a Pix that is still in process (newest first)
	g.gw.found = []payment.GatewayPayment{gp("1002", "in_process", "221"), gp("1001", "rejected", "221")}
	g.tx(0) // only the live attempt is applied: its id is remembered

	g.svc.ProcessGatewayWork(context.Background())

	got := g.reload()
	assert.Equal(t, payment.StatusPending, got.Status)
	assert.Equal(t, "1002", got.GatewayPaymentID)
	g.assertDB()
}

func TestGateway_ARefusalAloneStillRejects(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.found = []payment.GatewayPayment{gp("1001", "rejected", "221")}
	g.tx(1)
	g.tx(0) // the checkout is closed in the same pass

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, payment.StatusRejected, g.reload().Status)
	g.assertDB()
}

func TestGateway_ClosingACancelledPaymentCancelsAPixItNeverHeardOf(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusCancelled })
	g.gw.found = []payment.GatewayPayment{gp("1005", "in_process", "221")}
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, []string{"1005"}, g.gw.cancelled)
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	assert.NotNil(t, g.reload().CheckoutClosedAt)
	g.assertDB()
}

func TestGateway_ClosingACancelledPaymentFlagsAnApprovalItNeverHeardOf(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusRejected })
	g.gw.found = []payment.GatewayPayment{gp("1005", "approved", "221")}
	g.tx(0) // applying a late approval changes nothing and only logs that it needs a refund
	g.tx(0) // closing

	g.svc.ProcessGatewayWork(context.Background())

	assert.Empty(t, g.gw.cancelled)
	assert.Equal(t, payment.StatusRejected, g.reload().Status)
	g.assertDB()
}

func TestGateway_ClosingRetriesWhenTheLookForUnknownPaymentsFails(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusCancelled })
	g.gw.findErr = transient
	assert.ErrorIs(t, g.svc.closeCheckout(context.Background(), p), transient)
	assert.Empty(t, g.gw.expired)
}

func TestGateway_NotificationForAnUnknownGatewayPaymentIsAcknowledged(t *testing.T) {
	g := newGatewayHarness(t)
	g.gw.getErr = &payment.GatewayError{NotFound: true, Err: errors.New("not found")}
	res, err := g.svc.HandleGatewayNotification(context.Background(), "999")
	require.NoError(t, err)
	assert.Equal(t, ResultUnknown, res)

	g.gw.getErr = transient
	_, err = g.svc.HandleGatewayNotification(context.Background(), "999")
	assert.ErrorIs(t, err, transient, "a transient failure makes the gateway retry")

	g.gw.getErr = permanent // refused for another reason than "not found": not acknowledged
	_, err = g.svc.HandleGatewayNotification(context.Background(), "999")
	assert.ErrorIs(t, err, permanent)
}

func TestGateway_ManualDecisionIsRefusedBeforeTheCheckoutExistsToo(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(nil) // no checkout yet
	g.mock.ExpectBegin()
	g.mock.ExpectRollback()
	assert.ErrorIs(t, g.svc.Confirm(context.Background(), "pay-1", "req-1"), payment.ErrGatewayManaged)
	g.assertDB()
}

func TestGateway_AnotherApprovalIsAppliedEvenWhenAnEarlierOneCannotBeSettled(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.found = []payment.GatewayPayment{gp("1001", "approved", "200"), gp("1002", "approved", "221")}
	g.tx(0) // 1001: another amount, ignored
	g.tx(1) // 1002: settles

	err := g.svc.reconcile(context.Background(), g.reload())

	assert.ErrorContains(t, err, "1001", "the unsettled approval is still reported")
	assert.Equal(t, payment.StatusApproved, g.reload().Status)
	assert.Equal(t, "1002", g.reload().GatewayPaymentID)
	g.assertDB()
}

func TestGateway_ExpiryWithoutAnyCheckoutCancelsLocally(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(func(p *payment.Payment) { exp := t0.Add(-time.Hour); p.ExpiresAt = &exp })
	g.gw.createErr = &payment.GatewayError{Transient: true, Err: errors.New("401 unauthorized")} // the token never worked
	g.tx(1)                                                                                      // PaymentFailed
	require.NoError(t, g.svc.expire(context.Background(), p))
	assert.Equal(t, payment.StatusCancelled, g.reload().Status)
	assert.Zero(t, g.gw.finds, "there is no checkout to look at")
}

func TestGateway_ClosingStillExpiresTheCheckoutWhenTheSearchIsRefusedForGood(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.Status = payment.StatusCancelled })
	g.gw.findErr = permanent
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, []string{"pref-1"}, g.gw.expired)
	assert.NotNil(t, g.reload().CheckoutClosedAt)
	g.assertDB()
}

func TestGateway_PaymentsInBackoffGoToTheBackOfTheLine(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.findErr = transient
	g.svc.ProcessGatewayWork(context.Background()) // fails: backoff
	g.now = g.now.Add(time.Second)
	g.svc.ProcessGatewayWork(context.Background()) // skipped by the backoff, still stamped
	got, err := g.payments.ListWork(context.Background(), nil, payment.WorkReconcile, g.now.Add(time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 1, g.gw.finds)
}

// ---- metrics ----

func statusChanges(to, source string) float64 {
	return testutil.ToFloat64(metrics.PaymentStatusChanges.WithLabelValues(to, source))
}

func unsettled(reason string) float64 {
	return testutil.ToFloat64(metrics.PaymentsUnsettled.WithLabelValues(reason))
}

func TestGateway_MetricsCountStatusChangesBySource(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	before := statusChanges("APPROVED", "gateway")
	g.tx(1)
	_, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221"))
	require.NoError(t, err)
	assert.Equal(t, before+1, statusChanges("APPROVED", "gateway"))

	g.tx(0)
	_, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221")) // duplicate: not a change
	require.NoError(t, err)
	assert.Equal(t, before+1, statusChanges("APPROVED", "gateway"))
	g.assertDB()
}

func TestGateway_MetricsCountWorkerSettlementsAndManualDecisions(t *testing.T) {
	g := newGatewayHarness(t)
	p := g.seedGatewayPayment(func(p *payment.Payment) { exp := t0.Add(-time.Hour); p.ExpiresAt = &exp })
	workerBefore := statusChanges("CANCELLED", "worker")
	g.tx(1)
	require.NoError(t, g.svc.expire(context.Background(), p))
	assert.Equal(t, workerBefore+1, statusChanges("CANCELLED", "worker"))

	manualBefore := statusChanges("APPROVED", "manual")
	require.NoError(t, g.payments.Create(context.Background(), nil, &payment.Payment{ID: "pay-2", BudgetID: "b", ServiceOrderID: "SO-2", SagaID: "saga-2",
		Amount: decimal.RequireFromString("10"), Status: payment.StatusPending, Provider: payment.ProviderName}))
	g.tx(1)
	require.NoError(t, g.svc.Confirm(context.Background(), "pay-2", "req-1"))
	assert.Equal(t, manualBefore+1, statusChanges("APPROVED", "manual"))
	g.assertDB()
}

func TestGateway_MetricsFlagMoneyThatIsNotSettled(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	mismatch, extra, late := unsettled("amount_mismatch"), unsettled("extra_approval"), unsettled("late_approval")

	g.tx(0)
	_, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "200"))
	require.NoError(t, err)
	assert.Equal(t, mismatch+1, unsettled("amount_mismatch"))

	g.tx(1)
	_, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1002", "approved", "221"))
	require.NoError(t, err)
	g.tx(0)
	_, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1003", "approved", "221")) // a second approval
	require.NoError(t, err)
	assert.Equal(t, extra+1, unsettled("extra_approval"))

	other := g.seedGatewayPayment(func(p *payment.Payment) {
		p.ID = "pay-3"
		p.ServiceOrderID = "SO-3"
		withCheckout(p)
		p.Status = payment.StatusCancelled
	})
	g.tx(0)
	_, err = g.svc.ApplyGatewayPayment(context.Background(), payment.GatewayPayment{ID: "1004", Status: "approved", ExternalReference: other.ID, Amount: decimal.RequireFromString("221")})
	require.NoError(t, err)
	assert.Equal(t, late+1, unsettled("late_approval"))
	g.assertDB()
}

func TestGateway_MetricsCountWorkerErrorsByKind(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	transientBefore := testutil.ToFloat64(metrics.GatewayWorkerErrors.WithLabelValues("RECONCILE", "transient"))
	g.gw.findErr = transient

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, transientBefore+1, testutil.ToFloat64(metrics.GatewayWorkerErrors.WithLabelValues("RECONCILE", "transient")))

	// a refund that is only waiting for the gateway is not an error
	g2 := newGatewayHarness(t)
	g2.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusApproved, "1001"
		requested := t0
		p.RefundRequestedAt = &requested
	})
	g2.gw.byID["1001"] = gp("1001", "approved", "221")
	g2.tx(0)
	waiting := testutil.ToFloat64(metrics.GatewayWorkerErrors.WithLabelValues("REFUND", "transient"))
	g2.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, waiting, testutil.ToFloat64(metrics.GatewayWorkerErrors.WithLabelValues("REFUND", "transient")))
}

func TestGateway_MetricsPublishThePendingBacklog(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { withCheckout(p); p.CreatedAt = t0.Add(-10 * time.Minute) })
	g.gw.found = nil

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, 1.0, testutil.ToFloat64(metrics.PaymentsPending))
	assert.InDelta(t, 600, testutil.ToFloat64(metrics.PaymentsPendingOldestAge), 1)

	empty := newGatewayHarness(t)
	empty.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, 0.0, testutil.ToFloat64(metrics.PaymentsPending))
	assert.Equal(t, 0.0, testutil.ToFloat64(metrics.PaymentsPendingOldestAge), "no pending payment, no age")
}

// ---- log events ----

func captureLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	t.Cleanup(logger.ReplaceGlobal(zap.New(core)))
	return logs
}

func eventsOf(logs *observer.ObservedLogs) map[string]observer.LoggedEntry {
	out := map[string]observer.LoggedEntry{}
	for _, e := range logs.All() {
		if ev, ok := e.ContextMap()["event"].(string); ok {
			out[ev] = e
		}
	}
	return out
}

func TestGateway_LogsCarryEventNamesAndTheSagaIds(t *testing.T) {
	logs := captureLogs(t)
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.tx(1)
	_, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "approved", "221"))
	require.NoError(t, err)
	g.tx(0)
	_, err = g.svc.ApplyGatewayPayment(context.Background(), gp("1002", "approved", "221")) // a second approval
	require.NoError(t, err)

	got := eventsOf(logs)
	applied := got["payment.status_applied"]
	assert.Equal(t, "payment status applied from the gateway", applied.Message, "the message text stays stable")
	assert.Equal(t, "saga-1", applied.ContextMap()["saga_id"])
	assert.Equal(t, "SO-1", applied.ContextMap()["service_order_id"])
	unsettled := got["payment.unsettled"]
	assert.Equal(t, "extra_approval", unsettled.ContextMap()["reason"])
	g.assertDB()
}

func TestGateway_WorkerFailuresAreLoggedAsEvents(t *testing.T) {
	logs := captureLogs(t)
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.gw.findErr = transient

	g.svc.ProcessGatewayWork(context.Background())

	failed := eventsOf(logs)["gateway.worker_step_failed"]
	assert.Equal(t, "RECONCILE", failed.ContextMap()["step"])
	assert.Equal(t, true, failed.ContextMap()["transient"])
	assert.Equal(t, "saga-1", failed.ContextMap()["saga_id"])
}

func TestGateway_NotApplicablePaymentsAreLoggedWithAReason(t *testing.T) {
	logs := captureLogs(t)
	g := newGatewayHarness(t)
	g.seedGatewayPayment(withCheckout)
	g.tx(0)
	_, err := g.svc.ApplyGatewayPayment(context.Background(), gp("1001", "in_mediation", "221"))
	require.NoError(t, err)
	g.tx(0)
	_, err = g.svc.ApplyGatewayPayment(context.Background(), payment.GatewayPayment{ID: "9", Status: "approved", ExternalReference: "nope", Amount: decimal.NewFromInt(1)})
	require.NoError(t, err)

	var reasons []any
	for _, e := range logs.All() {
		if e.ContextMap()["event"] == "gateway.payment_not_applied" {
			reasons = append(reasons, e.ContextMap()["reason"])
		}
	}
	assert.ElementsMatch(t, []any{"status_not_mapped", "unknown_payment"}, reasons)
	g.assertDB()
}

// ---- work lock ----

type fakeLock struct {
	ok       bool
	err      error
	tried    int
	released int
}

func (f *fakeLock) TryLock(context.Context) (func(), bool, error) {
	f.tried++
	if f.err != nil || !f.ok {
		return nil, false, f.err
	}
	return func() { f.released++ }, true, nil
}

func passes(result string) float64 {
	return testutil.ToFloat64(metrics.GatewayWorkerPasses.WithLabelValues(result))
}

func TestGateway_APassWithoutTheLockDoesNothing(t *testing.T) {
	g := newGatewayHarness(t)
	lock := &fakeLock{ok: false}
	g.svc.workLock = lock
	g.seedGatewayPayment(nil) // would get its checkout opened
	skipped := passes("skipped")

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, 1, lock.tried)
	assert.Empty(t, g.gw.created, "another replica is running this pass")
	assert.Equal(t, skipped+1, passes("skipped"))
	g.assertDB()
}

func TestGateway_APassWithTheLockRunsAndGivesItBack(t *testing.T) {
	g := newGatewayHarness(t)
	lock := &fakeLock{ok: true}
	g.svc.workLock = lock
	g.seedGatewayPayment(nil)
	ran := passes("ran")
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Len(t, g.gw.created, 1)
	assert.Equal(t, 1, lock.released, "the lock goes back even though the pass did work")
	assert.Equal(t, ran+1, passes("ran"))
	g.assertDB()
}

func TestGateway_ALockThatCannotBeAskedSkipsThePassAndIsCounted(t *testing.T) {
	logs := captureLogs(t)
	g := newGatewayHarness(t)
	g.svc.workLock = &fakeLock{err: errors.New("db down")}
	g.seedGatewayPayment(nil)
	failed := passes("error")

	g.svc.ProcessGatewayWork(context.Background())

	assert.Empty(t, g.gw.created)
	assert.Equal(t, failed+1, passes("error"))
	entry := eventsOf(logs)["gateway.worker_failed"]
	assert.Equal(t, "LOCK", entry.ContextMap()["step"])
}

func TestGateway_WithGatewayKeepsTheWorkLock(t *testing.T) {
	g := newGatewayHarness(t)
	lock := &fakeLock{ok: true}
	g.svc.WithGateway(GatewayConfig{Gateway: g.gw, WorkLock: lock})
	assert.Same(t, lock, g.svc.workLock)
}

// ---- refund backlog ----

func seedRequestedRefund(g *gatewayHarness, requestedAt time.Time) {
	g.seedGatewayPayment(func(p *payment.Payment) {
		withCheckout(p)
		p.Status, p.GatewayPaymentID = payment.StatusApproved, "1001"
		p.RefundRequestedAt = &requestedAt
	})
	g.gw.byID["1001"] = gp("1001", "approved", "221") // Mercado Pago has not refunded it
}

func TestGateway_ARefundThatStaysUnconfirmedIsWarnedAboutAfterTheThreshold(t *testing.T) {
	logs := captureLogs(t)
	g := newGatewayHarness(t)
	seedRequestedRefund(g, t0)

	g.now = t0.Add(10 * time.Minute) // waiting is normal for a while
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.NotContains(t, eventsOf(logs), "payment.refund_stuck")

	g.now = t0.Add(31 * time.Minute)
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())

	stuck := eventsOf(logs)["payment.refund_stuck"]
	assert.Equal(t, zapcore.WarnLevel, stuck.Level, "warning, not the debug line of a step that waits")
	assert.Equal(t, "saga-1", stuck.ContextMap()["saga_id"])
	assert.Equal(t, "1001", stuck.ContextMap()["gateway_payment_id"])
	assert.InDelta(t, (31 * time.Minute).Seconds(), stuck.ContextMap()["age_seconds"], 1)
	g.assertDB()
}

func TestGateway_ARefusedRefundPastTheThresholdIsAlsoWarnedAbout(t *testing.T) {
	logs := captureLogs(t)
	g := newGatewayHarness(t)
	seedRequestedRefund(g, t0)
	g.gw.refundErr = permanent
	g.now = t0.Add(time.Hour)
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Contains(t, eventsOf(logs), "payment.refund_stuck")
}

func TestGateway_MetricsPublishTheRefundBacklog(t *testing.T) {
	g := newGatewayHarness(t)
	seedRequestedRefund(g, t0.Add(-20*time.Minute))
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, 1.0, testutil.ToFloat64(metrics.RefundsPending))
	assert.InDelta(t, (20 * time.Minute).Seconds(), testutil.ToFloat64(metrics.RefundsPendingOldestAge), 1)

	empty := newGatewayHarness(t)
	empty.svc.ProcessGatewayWork(context.Background())
	assert.Equal(t, 0.0, testutil.ToFloat64(metrics.RefundsPending))
	assert.Equal(t, 0.0, testutil.ToFloat64(metrics.RefundsPendingOldestAge), "no refund waiting, no age")
	g.assertDB()
}

// ---- a checkout whose creation went through but was never stored ----

func TestGateway_AnApprovalOnACheckoutThatWasNeverStoredIsStillFound(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(nil) // no external_id: the answer of CreateCheckout was lost
	g.gw.createErr = transient
	g.gw.found = []payment.GatewayPayment{gp("1001", "approved", "221")}

	g.tx(1)

	g.svc.ProcessGatewayWork(context.Background()) // the creation fails, and the same pass still reconciles

	got := g.reload()
	assert.Equal(t, payment.StatusApproved, got.Status, "reconciliation looks by external reference, with or without a stored checkout")
	assert.Equal(t, "1001", got.GatewayPaymentID)
	g.assertDB()
}

func TestGateway_ClosingAPaymentWithoutAStoredCheckoutExpiresTheOneTheGatewayHolds(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { p.Status = payment.StatusCancelled }) // cancelled before any checkout was stored
	g.gw.openCheckout = &payment.Checkout{ExternalID: "pref-orphan", URL: "https://mp/orphan"}
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Equal(t, 1, g.gw.findCheckouts)
	assert.Equal(t, []string{"pref-orphan"}, g.gw.expired, "the payable checkout nobody stored is closed")
	assert.NotNil(t, g.reload().CheckoutClosedAt)
	g.assertDB()
}

func TestGateway_ClosingAPaymentThatHasNoCheckoutAnywhereJustMarksItClosed(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { p.Status = payment.StatusRejected })
	g.tx(0)

	g.svc.ProcessGatewayWork(context.Background())

	assert.Empty(t, g.gw.expired)
	assert.NotNil(t, g.reload().CheckoutClosedAt)
	g.assertDB()
}

func TestGateway_ClosingRetriesWhenTheGatewayCannotBeAskedForTheCheckout(t *testing.T) {
	g := newGatewayHarness(t)
	g.seedGatewayPayment(func(p *payment.Payment) { p.Status = payment.StatusCancelled })
	g.gw.findCheckoutErr = transient

	g.svc.ProcessGatewayWork(context.Background())

	assert.Nil(t, g.reload().CheckoutClosedAt, "a transient failure is retried: the checkout may still be payable")
	assert.Empty(t, g.gw.expired)

	g.now = g.now.Add(time.Minute)
	g.gw.findCheckoutErr = permanent // refused for good: nothing more can be done, so it is closed
	g.tx(0)
	g.svc.ProcessGatewayWork(context.Background())
	assert.NotNil(t, g.reload().CheckoutClosedAt)
	g.assertDB()
}
