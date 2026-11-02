package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
)

const (
	workBatch       = 20
	baseBackoff     = 5 * time.Second
	maxBackoff      = 5 * time.Minute
	defaultCheckTTL = 48 * time.Hour
)

// GatewayConfig turns on the mercadopago mode of the service.
type GatewayConfig struct {
	Gateway         payment.Gateway
	NotificationURL string        // public HTTPS URL of the webhook; empty means "do not send"
	CheckoutTTL     time.Duration // lifetime of a checkout (48 h by default)
}

// WithGateway enables the gateway mode: RequestPayment records a PENDING payment and the gateway
// worker opens the checkout. The internal modes never call it.
func (s *Service) WithGateway(cfg GatewayConfig) *Service {
	s.gateway = cfg.Gateway
	s.notificationURL = cfg.NotificationURL
	s.checkoutTTL = cfg.CheckoutTTL
	if s.checkoutTTL <= 0 {
		s.checkoutTTL = defaultCheckTTL
	}
	return s
}

func (s *Service) providerName() string {
	if s.gateway != nil {
		return payment.ProviderMercadoPago
	}
	return payment.ProviderName
}

// ApplyResult says what ApplyGatewayPayment did with a gateway payment.
type ApplyResult string

const (
	ResultApplied   ApplyResult = "applied"
	ResultDuplicate ApplyResult = "duplicate"
	ResultIgnored   ApplyResult = "ignored"
	ResultUnknown   ApplyResult = "unknown"
)

// ApplyGatewayPayment is the single place where a gateway payment changes the status of a payment.
// The webhook, the reconciliation and the refund confirmation all call it, so they behave the same.
// It locks the payment row, validates the transition and enqueues the saga event in the same
// transaction. Repeating it is harmless.
func (s *Service) ApplyGatewayPayment(ctx context.Context, gp payment.GatewayPayment) (ApplyResult, error) {
	target, mapped := payment.MapGatewayStatus(gp.Status)
	result := ResultIgnored
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		pay, err := s.payments.FindByIDForUpdate(ctx, tx, gp.ExternalReference)
		if errors.Is(err, payment.ErrNotFound) {
			logger.Of(ctx).Warn("gateway payment of an unknown payment", zap.String("gateway_payment_id", gp.ID))
			result = ResultUnknown
			return nil
		}
		if err != nil {
			return err
		}
		if !mapped {
			logger.Of(ctx).Warn("gateway status not mapped, ignoring", zap.String("payment_id", pay.ID), zap.String("gateway_status", gp.Status))
			return nil
		}
		result, err = s.applyLocked(ctx, tx, pay, gp, target)
		return err
	})
	return result, err
}

func (s *Service) applyLocked(ctx context.Context, tx *sql.Tx, pay *payment.Payment, gp payment.GatewayPayment, target payment.Status) (ApplyResult, error) {
	base := []zap.Field{zap.String("payment_id", pay.ID), zap.String("service_order_id", pay.ServiceOrderID), zap.String("gateway_payment_id", gp.ID)}
	fields := func(extra ...zap.Field) []zap.Field { return append(slices.Clone(base), extra...) }

	switch {
	case target == payment.StatusApproved && pay.Status == payment.StatusApproved && pay.GatewayPaymentID != "" && pay.GatewayPaymentID != gp.ID:
		logger.Of(ctx).Error(errors.New("extra approved gateway payment: needs a manual refund"), fields(zap.String("settled_by", pay.GatewayPaymentID))...)
		return ResultIgnored, nil
	case target == payment.StatusApproved && (pay.Status == payment.StatusCancelled || pay.Status == payment.StatusRejected):
		logger.Of(ctx).Error(errors.New("late approval on a closed payment: needs a manual refund"), fields(zap.String("status", string(pay.Status)))...)
		return ResultIgnored, nil
	case target == payment.StatusApproved && !gp.Amount.Equal(pay.Amount):
		logger.Of(ctx).Error(errors.New("approved gateway payment with another amount, not settled"), fields(zap.String("expected", formatMoney(pay.Amount)), zap.String("got", formatMoney(gp.Amount)))...)
		return ResultIgnored, nil
	case target == pay.Status:
		if pay.GatewayPaymentID == "" && gp.ID != "" {
			pay.GatewayPaymentID = gp.ID // remembered so the expiry knows what to cancel
			if err := s.payments.Update(ctx, tx, pay); err != nil {
				return ResultIgnored, err
			}
		}
		return ResultDuplicate, nil
	}

	previous := pay.Status
	if err := pay.Transition(target); err != nil {
		logger.Of(ctx).Warn("gateway status ignored: invalid transition", fields(zap.String("from", string(previous)), zap.String("to", string(target)))...)
		return ResultIgnored, nil
	}
	if pay.GatewayPaymentID == "" || target == payment.StatusApproved {
		pay.GatewayPaymentID = gp.ID
	}
	if err := s.payments.Update(ctx, tx, pay); err != nil {
		return ResultIgnored, err
	}
	if err := s.publishOutcome(ctx, tx, pay, target); err != nil {
		return ResultIgnored, err
	}
	logger.Of(ctx).Info("payment status applied from the gateway", fields(zap.String("from", string(previous)), zap.String("to", string(target)))...)
	return ResultApplied, nil
}

// publishOutcome enqueues the saga event that goes with a final payment status. The contract has a
// single failure reason, PAYMENT_REJECTED, used for every failed payment.
func (s *Service) publishOutcome(ctx context.Context, tx *sql.Tx, pay *payment.Payment, status payment.Status) error {
	requestID := s.newID()
	switch status {
	case payment.StatusApproved:
		return s.publish(ctx, tx, pay.SagaID, pay.ServiceOrderID, requestID, evPaymentApproved,
			paymentApprovedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount)})
	case payment.StatusRejected, payment.StatusCancelled:
		return s.publish(ctx, tx, pay.SagaID, pay.ServiceOrderID, requestID, evPaymentFailed,
			paymentFailedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount), Reason: reasonPaymentRejected})
	case payment.StatusRefunded:
		return s.publish(ctx, tx, pay.SagaID, pay.ServiceOrderID, requestID, evPaymentRefunded,
			paymentRefundedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount)})
	}
	return nil
}

// ---- the gateway worker ----------------------------------------------------------------------

// errNotYet means the gateway has not finished something; it is retried without being an error.
var errNotYet = errors.New("not confirmed by the gateway yet")

// ProcessGatewayWork runs one pass of the gateway worker. Every step reads its work from the database,
// calls the gateway outside any transaction and records the result in a short transaction. A failure
// in one payment is retried with a backoff and never blocks the others.
func (s *Service) ProcessGatewayWork(ctx context.Context) {
	if s.gateway == nil {
		return
	}
	now := s.now()
	steps := []struct {
		kind payment.WorkKind
		run  func(context.Context, *payment.Payment) error
	}{
		{payment.WorkCreateCheckout, s.createCheckout},
		{payment.WorkReconcile, s.reconcile},
		{payment.WorkExpire, s.expire}, // after reconcile: a payment the gateway approved wins over the expiry
		{payment.WorkRefund, s.refund},
		{payment.WorkCloseCheckout, s.closeCheckout},
	}
	for _, step := range steps {
		list, err := s.payments.ListWork(ctx, s.db, step.kind, now, workBatch)
		if err != nil {
			logger.Of(ctx).Error(fmt.Errorf("list %s: %w", step.kind, err), zap.String("component", "gateway-worker"))
			continue
		}
		for _, p := range list {
			key := string(step.kind) + ":" + p.ID
			if !s.retry.ready(key, now) {
				continue
			}
			if err := step.run(ctx, p); err != nil {
				s.retry.fail(key, now)
				s.logStepError(ctx, step.kind, p, err)
				continue
			}
			s.retry.clear(key)
		}
	}
}

func (s *Service) logStepError(ctx context.Context, kind payment.WorkKind, p *payment.Payment, err error) {
	fields := []zap.Field{zap.String("component", "gateway-worker"), zap.String("step", string(kind)), zap.String("payment_id", p.ID)}
	if errors.Is(err, errNotYet) {
		logger.Of(ctx).Debug("waiting for the gateway", fields...)
		return
	}
	logger.Of(ctx).Error(err, append(fields, zap.Bool("transient", payment.IsTransient(err)))...)
}

// RunGatewayWorker calls ProcessGatewayWork every interval until ctx is cancelled.
func (s *Service) RunGatewayWorker(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.ProcessGatewayWork(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// createCheckout opens the hosted checkout of a payment that has none. A permanent refusal means the
// payment can never be paid, so it fails like a rejection.
func (s *Service) createCheckout(ctx context.Context, p *payment.Payment) error {
	expires := s.now().Add(s.checkoutTTL)
	if p.ExpiresAt != nil {
		expires = *p.ExpiresAt
	}
	co, err := s.gateway.CreateCheckout(ctx, payment.CheckoutRequest{
		PaymentID:       p.ID,
		Title:           "Ordem de serviço " + p.ServiceOrderID,
		Amount:          p.Amount,
		NotificationURL: s.notificationURL,
		ExpiresAt:       expires,
	})
	if err != nil {
		if payment.IsTransient(err) {
			return err
		}
		logger.Of(ctx).Error(err, zap.String("payment_id", p.ID), zap.String("component", "gateway-worker"))
		return s.settle(ctx, p.ID, payment.StatusRejected, func(pay *payment.Payment) bool { return pay.ExternalID == "" })
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		pay, err := s.payments.FindByIDForUpdate(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		if pay.ExternalID != "" {
			return nil // another replica was faster; the idempotency key made both calls return the same checkout
		}
		// The payment may have been cancelled meanwhile: the checkout is stored anyway so it gets closed.
		pay.ExternalID, pay.CheckoutURL = co.ExternalID, co.URL
		return s.payments.Update(ctx, tx, pay)
	})
}

// reconcile asks the gateway for the payments of a checkout and applies them, approved first.
func (s *Service) reconcile(ctx context.Context, p *payment.Payment) error {
	found, err := s.gateway.FindPayments(ctx, p.ID)
	if err != nil {
		return err
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Status == "approved" && found[j].Status != "approved" })
	for _, gp := range found {
		gp.ExternalReference = p.ID
		if _, err := s.ApplyGatewayPayment(ctx, gp); err != nil {
			return err
		}
	}
	return nil
}

// expire ends a checkout whose lifetime has passed. A payment that never reached the gateway is
// CANCELLED; a payment the gateway still holds (for example in_process) is cancelled there first and
// then REJECTED. Both answer the saga with PaymentFailed. An approval found first always wins.
func (s *Service) expire(ctx context.Context, p *payment.Payment) error {
	if err := s.reconcile(ctx, p); err != nil {
		return err
	}
	pay, err := s.payments.FindByID(ctx, s.db, p.ID)
	if err != nil {
		return err
	}
	if pay.Status != payment.StatusPending {
		return nil
	}
	target := payment.StatusCancelled
	if pay.GatewayPaymentID != "" {
		if err := s.gateway.CancelPayment(ctx, pay.GatewayPaymentID); err != nil {
			return err // transient or not, the status may have changed: retry after reconciling again
		}
		target = payment.StatusRejected
	}
	return s.settle(ctx, pay.ID, target, nil)
}

// refund asks the gateway to refund an approved payment and settles it only once the gateway says so.
func (s *Service) refund(ctx context.Context, p *payment.Payment) error {
	if p.GatewayPaymentID == "" {
		return errors.New("refund requested for a payment without a gateway payment id")
	}
	if !s.retry.refundSubmitted(p.ID) {
		if err := s.gateway.Refund(ctx, p.GatewayPaymentID); err != nil {
			return err
		}
		s.retry.markRefundSubmitted(p.ID)
	}
	gp, err := s.gateway.GetPayment(ctx, p.GatewayPaymentID)
	if err != nil {
		return err
	}
	gp.ExternalReference = p.ID
	if _, err := s.ApplyGatewayPayment(ctx, gp); err != nil {
		return err
	}
	if st, ok := payment.MapGatewayStatus(gp.Status); !ok || st != payment.StatusRefunded {
		return errNotYet
	}
	return nil
}

// closeCheckout makes the checkout of a finished payment unpayable, and cancels a gateway payment
// that is still in process for a payment that was cancelled. Permanent gateway answers (already
// final, checkout gone) count as done so the step does not retry forever.
func (s *Service) closeCheckout(ctx context.Context, p *payment.Payment) error {
	if p.Status == payment.StatusCancelled && p.GatewayPaymentID != "" {
		if err := s.gateway.CancelPayment(ctx, p.GatewayPaymentID); err != nil && payment.IsTransient(err) {
			return err
		}
	}
	if err := s.gateway.ExpireCheckout(ctx, p.ExternalID); err != nil && payment.IsTransient(err) {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		pay, err := s.payments.FindByIDForUpdate(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		closed := s.now()
		pay.CheckoutClosedAt = &closed
		return s.payments.Update(ctx, tx, pay)
	})
}

// settle moves a PENDING payment to a final status and answers the saga, unless it is no longer
// PENDING or guard says no.
func (s *Service) settle(ctx context.Context, id string, to payment.Status, guard func(*payment.Payment) bool) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		pay, err := s.payments.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if pay.Status != payment.StatusPending || (guard != nil && !guard(pay)) {
			return nil
		}
		if err := s.setStatus(ctx, tx, pay, to); err != nil {
			return err
		}
		return s.publishOutcome(ctx, tx, pay, to)
	})
}

// inTx runs fn in a transaction that commits when fn returns nil.
func (s *Service) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- in-memory retry state -------------------------------------------------------------------

// retryState keeps a backoff per failing step and remembers which refunds were already submitted.
// It lives in memory: after a restart a step simply runs again, and the gateway calls are idempotent.
type retryState struct {
	mu        sync.Mutex
	entries   map[string]retryEntry
	submitted map[string]bool
}

type retryEntry struct {
	attempts int
	next     time.Time
}

func (r *retryState) ready(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	return !ok || !now.Before(e.next)
}

func (r *retryState) fail(key string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = map[string]retryEntry{}
	}
	e := r.entries[key]
	e.attempts++
	delay := baseBackoff << min(e.attempts-1, 10)
	if delay > maxBackoff || delay <= 0 {
		delay = maxBackoff
	}
	e.next = now.Add(delay)
	r.entries[key] = e
}

func (r *retryState) clear(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
}

func (r *retryState) refundSubmitted(paymentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submitted[paymentID]
}

func (r *retryState) markRefundSubmitted(paymentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.submitted == nil {
		r.submitted = map[string]bool{}
	}
	r.submitted[paymentID] = true
}
