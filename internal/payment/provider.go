package payment

import (
	"context"
	"fmt"
)

// Outcome is the result of charging a payment at the provider.
type Outcome string

const (
	OutcomeApproved Outcome = "APPROVED"
	OutcomeRejected Outcome = "REJECTED"
	OutcomePending  Outcome = "PENDING"
)

// PaymentProvider charges and refunds payments. Implementations: InternalProvider (PAYMENT_MODE).
type PaymentProvider interface {
	Charge(ctx context.Context, p Payment) (Outcome, error)
	Refund(ctx context.Context, p Payment) error
}

// Mode is the PAYMENT_MODE of the internal provider.
type Mode string

const (
	ModeManual      Mode = "manual"
	ModeAutoApprove Mode = "auto-approve"
	ModeAutoReject  Mode = "auto-reject"
)

// ParseMode maps PAYMENT_MODE to a Mode. Empty means manual.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "":
		return ModeManual, nil
	case ModeManual, ModeAutoApprove, ModeAutoReject:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("invalid PAYMENT_MODE %q", s)
	}
}

// InternalProvider simulates the payment gateway according to its Mode.
type InternalProvider struct {
	mode Mode
}

func NewInternalProvider(mode Mode) *InternalProvider {
	return &InternalProvider{mode: mode}
}

func (p *InternalProvider) Charge(_ context.Context, _ Payment) (Outcome, error) {
	switch p.mode {
	case ModeAutoApprove:
		return OutcomeApproved, nil
	case ModeAutoReject:
		return OutcomeRejected, nil
	case ModeManual:
		return OutcomePending, nil
	default:
		return "", fmt.Errorf("unsupported PAYMENT_MODE %q", p.mode)
	}
}

// Refund is a no-op for the internal provider.
func (p *InternalProvider) Refund(_ context.Context, _ Payment) error {
	return nil
}
