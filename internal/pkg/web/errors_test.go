package web

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
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
		{"payment not found", fmt.Errorf("wrap: %w", payment.ErrNotFound), http.StatusNotFound, "wrap: payment not found"},
		{"invalid transition", fmt.Errorf("%w: A -> B", payment.ErrInvalidTransition), http.StatusConflict, payment.ErrInvalidTransition.Error()},
		{"data conflict", app.ErrDataConflict, http.StatusConflict, app.ErrDataConflict.Error()},
		{"data violation", app.ErrDataViolation, http.StatusUnprocessableEntity, app.ErrDataViolation.Error()},
		{"json syntax", jsonv2.ErrJSONSyntax, http.StatusBadRequest, ""},
		{"gateway managed payment", payment.ErrGatewayManaged, http.StatusConflict, payment.ErrGatewayManaged.Error()},
		{"invalid webhook signature", payment.ErrInvalidSignature, http.StatusUnauthorized, "Unauthorized"},
		{"invalid webhook notification", payment.ErrInvalidNotification, http.StatusBadRequest, payment.ErrInvalidNotification.Error()},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "boom"},
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
