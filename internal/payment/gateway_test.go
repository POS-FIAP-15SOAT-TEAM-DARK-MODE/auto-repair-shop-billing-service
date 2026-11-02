package payment

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapGatewayStatus(t *testing.T) {
	tests := map[string]struct {
		want   Status
		mapped bool
	}{
		"pending":      {StatusPending, true},
		"in_process":   {StatusPending, true},
		"authorized":   {StatusPending, true},
		"approved":     {StatusApproved, true},
		"rejected":     {StatusRejected, true},
		"cancelled":    {StatusCancelled, true},
		"refunded":     {StatusRefunded, true},
		"charged_back": {StatusRefunded, true},
		"in_mediation": {"", false},
		"":             {"", false},
	}
	for in, tt := range tests {
		got, ok := MapGatewayStatus(in)
		assert.Equal(t, tt.want, got, in)
		assert.Equal(t, tt.mapped, ok, in)
	}
}

func TestIsTransient(t *testing.T) {
	assert.True(t, IsTransient(&GatewayError{Transient: true, Err: errors.New("x")}))
	assert.False(t, IsTransient(&GatewayError{Transient: false, Err: errors.New("x")}))
	assert.True(t, IsTransient(fmt.Errorf("wrapped: %w", &GatewayError{Transient: true, Err: errors.New("x")})))
	assert.False(t, IsTransient(fmt.Errorf("wrapped: %w", &GatewayError{Err: errors.New("x")})))
	assert.True(t, IsTransient(errors.New("unknown errors are retried")))

	inner := errors.New("boom")
	ge := &GatewayError{Err: inner}
	assert.ErrorIs(t, ge, inner)
	assert.Equal(t, "boom", ge.Error())
}

func TestGatewayManaged(t *testing.T) {
	assert.False(t, (&Payment{}).GatewayManaged())
	assert.False(t, (&Payment{Provider: ProviderName, ExternalID: "pref-1"}).GatewayManaged())
	assert.True(t, (&Payment{Provider: ProviderMercadoPago}).GatewayManaged(), "even before the checkout exists")
}
