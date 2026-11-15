package web

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/json"
	"github.com/stretchr/testify/assert"
)

func TestError_StatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		msg    string
	}{
		{"budget not found", budget.ErrNotFound, http.StatusNotFound, "budget not found"},
		{"payment not found", fmt.Errorf("wrap: %w", payment.ErrNotFound), http.StatusNotFound, "payment not found"},
		{"invalid transition", fmt.Errorf("%w: A -> B", payment.ErrInvalidTransition), http.StatusConflict, payment.ErrInvalidTransition.Error()},
		{"data conflict", app.ErrDataConflict, http.StatusConflict, "conflict"},
		{"data violation", app.ErrDataViolation, http.StatusUnprocessableEntity, "invalid data"},
		{"json syntax", jsonv2.ErrJSONSyntax, http.StatusBadRequest, ""},
		{"gateway managed payment", payment.ErrGatewayManaged, http.StatusConflict, payment.ErrGatewayManaged.Error()},
		{"invalid webhook signature", payment.ErrInvalidSignature, http.StatusUnauthorized, "Unauthorized"},
		{"invalid webhook notification", payment.ErrInvalidNotification, http.StatusBadRequest, payment.ErrInvalidNotification.Error()},
		{"gateway failure the gateway must retry", &payment.GatewayError{Transient: true, Err: errors.New("down")}, http.StatusInternalServerError, "internal server error"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal server error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := Error(tc.err)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, status, body.StatusCode)
			if tc.msg != "" {
				assert.Contains(t, body.Error+fmt.Sprint(body.Errors), tc.msg)
			}
		})
	}
}

// rootCause mimics a Postgres driver error that must never reach a response body.
var rootCause = errors.New(`pq: duplicate key value violates unique constraint "payment_pkey" (value 42)`)

func TestError_InfrastructureCauseNeverInBody(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"500 unknown wrap", fmt.Errorf("save: %w", rootCause), http.StatusInternalServerError, "internal server error"},
		{"500 joined", errors.Join(errors.New("a"), rootCause), http.StatusInternalServerError, "internal server error"},
		{"409 infra wrap", fmt.Errorf("%w: %w", app.ErrDataConflict, rootCause), http.StatusConflict, "conflict"},
		{"409 infra generic", fmt.Errorf("%w: %w", app.ErrInfraConflict, rootCause), http.StatusConflict, "conflict"},
		{"422 infra wrap", fmt.Errorf("%w: %w", app.ErrDataViolation, rootCause), http.StatusUnprocessableEntity, "invalid data"},
		{"404 wrap with cause", fmt.Errorf("%w: %w", budget.ErrNotFound, rootCause), http.StatusNotFound, "budget not found"},
		{"409 domain keeps sentinel message", fmt.Errorf("%w: %w", payment.ErrInvalidTransition, rootCause), http.StatusConflict, payment.ErrInvalidTransition.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := Error(tc.err)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, status, body.StatusCode)
			assert.Equal(t, tc.message, body.Error)
			assert.Empty(t, body.Errors)
			assert.NotContains(t, body.Error, "pq:")
			assert.NotContains(t, body.Error, "duplicate key")
			assert.NotContains(t, body.Error, "payment_pkey")
		})
	}
}

func TestShouldLog(t *testing.T) {
	assert.True(t, ShouldLog(rootCause, http.StatusInternalServerError))
	assert.True(t, ShouldLog(app.ErrDataConflict, http.StatusConflict))
	assert.True(t, ShouldLog(app.ErrDataViolation, http.StatusUnprocessableEntity))
	assert.False(t, ShouldLog(payment.ErrInvalidTransition, http.StatusConflict))
	assert.False(t, ShouldLog(payment.ErrGatewayManaged, http.StatusConflict), "a refused manual decision is a domain answer, not a failure")
	assert.False(t, ShouldLog(budget.ErrNotFound, http.StatusNotFound))
	assert.False(t, ShouldLog(jsonv2.ErrJSONSyntax, http.StatusBadRequest))
}

func TestError_DomainValidationAnswers400(t *testing.T) {
	cases := []struct {
		name string
		err  error
		msg  string
	}{
		{"customer required", budget.ErrCustomerRequired, "customer user id is required"},
		{"no works", budget.ErrNoWorks, "budget must have at least one work"},
		{"negative price", budget.ErrNegativePrice, "unit price cannot be negative"},
		{"invalid quantity", budget.ErrInvalidQuantity, "quantity must be greater than zero"},
		{"invalid payload", billing.ErrInvalidPayload, "invalid message payload"},
		{"invalid payload wrapped", fmt.Errorf("%w: money %q", billing.ErrInvalidPayload, "x"), "invalid message payload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := Error(tc.err)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, http.StatusBadRequest, body.StatusCode)
			assert.Empty(t, body.Error)
			assert.Equal(t, []string{tc.msg}, body.Errors)
		})
	}
}

func TestError_ValidationListKeepsOnlySentinelMessages(t *testing.T) {
	status, body := Error(fmt.Errorf("%w: %w", budget.ErrInvalidQuantity, rootCause))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, []string{budget.ErrInvalidQuantity.Error()}, body.Errors)
	assert.NotContains(t, fmt.Sprint(body.Errors), "pq:")
}
