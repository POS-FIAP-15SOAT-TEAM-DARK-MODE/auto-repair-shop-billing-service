package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
	"go.uber.org/zap"
)

// Service applies billing commands and REST decisions. Every state change and its
// outbox event are written in one transaction.
type Service struct {
	db       *sql.DB
	budgets  budget.Repository
	payments payment.Repository
	provider payment.PaymentProvider
	newID    func() string
	now      func() time.Time

	// gateway is set only in mercadopago mode (see WithGateway); the internal modes leave it nil.
	gateway         payment.Gateway
	notificationURL string
	checkoutTTL     time.Duration
	workLock        WorkLock
	retry           retryState
}

func NewService(
	database *sql.DB,
	budgets budget.Repository,
	payments payment.Repository,
	provider payment.PaymentProvider,
	newID func() string,
) *Service {
	return &Service{db: database, budgets: budgets, payments: payments, provider: provider, newID: newID, now: time.Now}
}

// RegisterHandlers binds the saga commands to the consumer.
func (s *Service) RegisterHandlers(c *messaging.Consumer) {
	c.RegisterHandler(cmdGenerateBudget, s.GenerateBudget)
	c.RegisterHandler(cmdCancelBudget, s.CancelBudget)
	c.RegisterHandler(cmdRequestPayment, s.RequestPayment)
	c.RegisterHandler(cmdRefundPayment, s.RefundPayment)
}

// GenerateBudget creates the budget unless one is already GENERATED for the service order.
func (s *Service) GenerateBudget(ctx context.Context, tx *sql.Tx, env messaging.Envelope) error {
	var p generateBudgetPayload
	if err := decodePayload(env, &p); err != nil {
		return err
	}
	works, supplies, err := toLines(p)
	if err != nil {
		return err
	}
	b, err := budget.NewBudget(s.newID, env.ServiceOrderID, p.CustomerUserID, works, supplies)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}

	_, err = s.budgets.FindGeneratedByServiceOrder(ctx, tx, env.ServiceOrderID)
	switch {
	case err == nil:
		logger.Of(ctx).Info("budget already generated, nothing to do", zap.String("event", "budget.already_generated"))
		return nil
	case !errors.Is(err, budget.ErrNotFound):
		return err
	}

	if err := s.budgets.Create(ctx, tx, b); err != nil {
		return err
	}
	return s.publish(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, evBudgetGenerated,
		budgetGeneratedPayload{BudgetID: b.ID, TotalAmount: formatMoney(b.TotalAmount)})
}

// CancelBudget moves the GENERATED budget of the service order to CANCELLED.
func (s *Service) CancelBudget(ctx context.Context, tx *sql.Tx, env messaging.Envelope) error {
	b, err := s.budgets.FindGeneratedByServiceOrder(ctx, tx, env.ServiceOrderID)
	if errors.Is(err, budget.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !b.Cancel() {
		return nil
	}
	if err := s.budgets.UpdateStatus(ctx, tx, b.ID, b.Status); err != nil {
		return err
	}
	return s.publish(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, evBudgetCancelled,
		budgetCancelledPayload{BudgetID: b.ID})
}

// RequestPayment charges the amount that equals the GENERATED budget total.
// Invalid requests are logged and discarded (no error, no redelivery).
func (s *Service) RequestPayment(ctx context.Context, tx *sql.Tx, env messaging.Envelope) error {
	var p requestPaymentPayload
	if err := decodePayload(env, &p); err != nil {
		return err
	}
	amount, err := parseMoney(p.Amount)
	if err != nil {
		return err
	}

	b, err := s.budgets.FindGeneratedByServiceOrder(ctx, tx, env.ServiceOrderID)
	if errors.Is(err, budget.ErrNotFound) {
		logger.Of(ctx).Warn("request payment discarded: no generated budget", zap.String("event", "payment.request_discarded"), zap.String("reason", "no_generated_budget"))
		return nil
	}
	if err != nil {
		return err
	}
	if !amount.Equal(b.TotalAmount) {
		logger.Of(ctx).Warn("request payment discarded: amount differs from budget total", zap.String("event", "payment.request_discarded"), zap.String("reason", "amount_differs"))
		return nil
	}

	latest, err := s.payments.FindLatestByServiceOrder(ctx, tx, env.ServiceOrderID)
	switch {
	case err == nil:
		if latest.Status == payment.StatusPending || latest.Status == payment.StatusApproved {
			logger.Of(ctx).Warn("request payment discarded: open payment exists", zap.String("event", "payment.request_discarded"), zap.String("reason", "open_payment_exists"))
			return nil
		}
	case !errors.Is(err, payment.ErrNotFound):
		return err
	}

	pay := &payment.Payment{
		ID:             s.newID(),
		BudgetID:       b.ID,
		ServiceOrderID: env.ServiceOrderID,
		SagaID:         env.SagaID,
		Amount:         amount,
		Status:         payment.StatusPending,
		Provider:       s.providerName(),
	}
	if s.gateway != nil {
		expires := s.now().Add(s.checkoutTTL)
		pay.ExpiresAt = &expires
	}
	if err := s.payments.Create(ctx, tx, pay); err != nil {
		return err
	}
	if s.gateway != nil {
		// The checkout is opened by the gateway worker once this transaction has committed: a remote
		// call must not hold the database transaction (ADR 0002).
		return nil
	}

	outcome, err := s.provider.Charge(ctx, *pay)
	if err != nil {
		return err
	}
	switch outcome {
	case payment.OutcomeApproved:
		if err := s.setStatus(ctx, tx, pay, payment.StatusApproved); err != nil {
			return err
		}
		return s.publish(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, evPaymentApproved,
			paymentApprovedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount)})
	case payment.OutcomeRejected:
		if err := s.setStatus(ctx, tx, pay, payment.StatusRejected); err != nil {
			return err
		}
		return s.publish(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, evPaymentFailed,
			paymentFailedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount), Reason: reasonPaymentRejected})
	case payment.OutcomePending:
		return nil
	default:
		return fmt.Errorf("unknown payment outcome %q", outcome)
	}
}

// RefundPayment cancels a PENDING payment or refunds an APPROVED one. Other states are ignored.
func (s *Service) RefundPayment(ctx context.Context, tx *sql.Tx, env messaging.Envelope) error {
	pay, err := s.payments.FindLatestByServiceOrder(ctx, tx, env.ServiceOrderID)
	if errors.Is(err, payment.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Lock the row: the webhook and the worker may be changing the same payment.
	if pay, err = s.payments.FindByIDForUpdate(ctx, tx, pay.ID); err != nil {
		return err
	}

	switch pay.Status {
	case payment.StatusPending:
		// For a gateway payment the worker closes the checkout (and cancels an in-process gateway payment).
		return s.setStatus(ctx, tx, pay, payment.StatusCancelled)
	case payment.StatusApproved:
		if pay.Provider == payment.ProviderMercadoPago {
			// Refund at the gateway first; PaymentRefunded is published only when the gateway confirms it.
			if pay.RefundRequestedAt == nil {
				requested := s.now()
				pay.RefundRequestedAt = &requested
				return s.payments.Update(ctx, tx, pay)
			}
			return nil
		}
		if err := pay.Transition(payment.StatusRefunded); err != nil {
			return err
		}
		if err := s.provider.Refund(ctx, *pay); err != nil {
			return err
		}
		if err := s.payments.Update(ctx, tx, pay); err != nil {
			return err
		}
		return s.publish(ctx, tx, env.SagaID, env.ServiceOrderID, env.RequestID, evPaymentRefunded,
			paymentRefundedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount)})
	default:
		return nil
	}
}

// Confirm approves a PENDING payment decided by the attendant (REST).
func (s *Service) Confirm(ctx context.Context, paymentID, requestID string) error {
	return s.decide(ctx, paymentID, requestID, payment.StatusApproved)
}

// Reject declines a PENDING payment decided by the attendant (REST).
func (s *Service) Reject(ctx context.Context, paymentID, requestID string) error {
	return s.decide(ctx, paymentID, requestID, payment.StatusRejected)
}

func (s *Service) decide(ctx context.Context, paymentID, requestID string, to payment.Status) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	pay, err := s.payments.FindByIDForUpdate(ctx, tx, paymentID)
	if err != nil {
		return err
	}
	if pay.GatewayManaged() {
		return payment.ErrGatewayManaged
	}
	if err := s.setStatus(ctx, tx, pay, to); err != nil {
		return err
	}

	switch to {
	case payment.StatusApproved:
		err = s.publish(ctx, tx, pay.SagaID, pay.ServiceOrderID, requestID, evPaymentApproved,
			paymentApprovedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount)})
	case payment.StatusRejected:
		err = s.publish(ctx, tx, pay.SagaID, pay.ServiceOrderID, requestID, evPaymentFailed,
			paymentFailedPayload{PaymentID: pay.ID, Amount: formatMoney(pay.Amount), Reason: reasonPaymentRejected})
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	metrics.PaymentStatusChanges.WithLabelValues(string(to), "manual").Inc()
	return nil
}

// GetBudget returns the GENERATED budget of a service order.
func (s *Service) GetBudget(ctx context.Context, serviceOrderID string) (*budget.Budget, error) {
	return s.budgets.FindGeneratedByServiceOrder(ctx, s.db, serviceOrderID)
}

// GetLatestPayment returns the most recent payment of a service order.
func (s *Service) GetLatestPayment(ctx context.Context, serviceOrderID string) (*payment.Payment, error) {
	return s.payments.FindLatestByServiceOrder(ctx, s.db, serviceOrderID)
}

// setStatus applies a validated transition and persists it.
func (s *Service) setStatus(ctx context.Context, ex db.Executor, pay *payment.Payment, to payment.Status) error {
	if err := pay.Transition(to); err != nil {
		return err
	}
	return s.payments.Update(ctx, ex, pay)
}

// publish enqueues an event carrying the saga, service order and request ids.
func (s *Service) publish(ctx context.Context, tx *sql.Tx, sagaID, serviceOrderID, requestID, typ string, payload any) error {
	ev, err := messaging.NewEnvelope(typ, sagaID, serviceOrderID, requestID, payload)
	if err != nil {
		return err
	}
	if err := messaging.Enqueue(ctx, tx, topicBillingEvents, "", ev); err != nil {
		return err
	}
	logger.Of(ctx).Info("event enqueued", zap.String("event", eventNames[typ]))
	return nil
}

func decodePayload(env messaging.Envelope, into any) error {
	if err := json.Unmarshal(env.Payload, into); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	return nil
}

func toLines(p generateBudgetPayload) ([]budget.WorkLine, []budget.SupplyLine, error) {
	works := make([]budget.WorkLine, 0, len(p.Works))
	for _, w := range p.Works {
		price, err := parseMoney(w.UnitPrice)
		if err != nil {
			return nil, nil, err
		}
		works = append(works, budget.WorkLine{WorkID: w.WorkID, Name: w.Name, UnitPrice: price})
	}
	supplies := make([]budget.SupplyLine, 0, len(p.Supplies))
	for _, s := range p.Supplies {
		price, err := parseMoney(s.UnitPrice)
		if err != nil {
			return nil, nil, err
		}
		supplies = append(supplies, budget.SupplyLine{SupplyID: s.SupplyID, Name: s.Name, Quantity: s.Quantity, UnitPrice: price})
	}
	return works, supplies, nil
}
