package payment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var allStatuses = []Status{StatusPending, StatusApproved, StatusRejected, StatusRefunded, StatusCancelled}

// expected transitions; everything else must fail
var validTransitions = map[[2]Status]bool{
	{StatusPending, StatusApproved}:  true,
	{StatusPending, StatusRejected}:  true,
	{StatusPending, StatusCancelled}: true,
	{StatusApproved, StatusRefunded}: true,
}

func TestTransition_AllPairs(t *testing.T) {
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				p := &Payment{Status: from}
				err := p.Transition(to)
				if validTransitions[[2]Status{from, to}] {
					require.NoError(t, err)
					assert.Equal(t, to, p.Status)
					return
				}
				assert.ErrorIs(t, err, ErrInvalidTransition)
				assert.Equal(t, from, p.Status, "status must not change on invalid transition")
			})
		}
	}
}

func TestTransition_RepeatedIsRejected(t *testing.T) {
	p := &Payment{Status: StatusPending}
	require.NoError(t, p.Transition(StatusApproved))
	assert.ErrorIs(t, p.Transition(StatusApproved), ErrInvalidTransition)
	require.NoError(t, p.Transition(StatusRefunded))
	assert.ErrorIs(t, p.Transition(StatusRefunded), ErrInvalidTransition)
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"": ModeManual, "manual": ModeManual, "auto-approve": ModeAutoApprove, "auto-reject": ModeAutoReject} {
		got, err := ParseMode(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	_, err := ParseMode("bogus")
	assert.Error(t, err)
}

func TestInternalProvider_Charge(t *testing.T) {
	ctx := context.Background()
	cases := map[Mode]Outcome{
		ModeAutoApprove: OutcomeApproved,
		ModeAutoReject:  OutcomeRejected,
		ModeManual:      OutcomePending,
	}
	for mode, want := range cases {
		got, err := NewInternalProvider(mode).Charge(ctx, Payment{})
		require.NoError(t, err)
		assert.Equal(t, want, got, string(mode))
	}

	_, err := NewInternalProvider(Mode("weird")).Charge(ctx, Payment{})
	assert.Error(t, err)
}

func TestInternalProvider_Refund(t *testing.T) {
	assert.NoError(t, NewInternalProvider(ModeManual).Refund(context.Background(), Payment{}))
}
