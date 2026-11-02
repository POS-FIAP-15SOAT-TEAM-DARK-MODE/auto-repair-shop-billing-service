package web

import (
	"errors"
	"net/http"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/json"
)

type errorResponse struct {
	StatusCode int      `json:"code"`
	Errors     []string `json:"errors,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Error builds an errorResponse for the given error and decides the status code.
func Error(err error) (int, errorResponse) {
	status := getHTTPStatus(err)
	stackedErrors, errorMsg := buildErrorMessages(err, status)

	return status, errorResponse{
		StatusCode: status,
		Errors:     stackedErrors,
		Error:      errorMsg,
	}
}

// ShouldLog reports whether the full error must go to the server log: every 5xx and
// the 409/422 answered with a generic message (the client never sees their cause).
func ShouldLog(err error, status int) bool {
	switch status {
	case http.StatusConflict:
		return !errors.Is(err, payment.ErrInvalidTransition)
	case http.StatusUnprocessableEntity:
		return true
	default:
		return status >= http.StatusInternalServerError
	}
}

// getHTTPStatus determines the HTTP status code for a given error.
func getHTTPStatus(err error) int {
	switch {
	case isValidationError(err):
		return http.StatusBadRequest
	case isNotFoundError(err):
		return http.StatusNotFound
	case isInternalServerError(err):
		return http.StatusInternalServerError
	case isConflictError(err):
		return http.StatusConflict
	case isUnprocessableEntityError(err):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// Generic messages returned to clients for infrastructure failures. The real cause
// is logged by the transport adapters (see routing), never sent in the body.
const (
	msgInternal    = "internal server error"
	msgConflict    = "conflict"
	msgInvalidData = "invalid data"
	msgNotFound    = "not found"
)

// buildErrorMessages formats the error messages based on status code.
// Only domain sentinels reach the body; any other error is answered generically.
func buildErrorMessages(err error, status int) ([]string, string) {
	switch status {
	case http.StatusBadRequest:
		return validationMessages(err), ""
	case http.StatusNotFound:
		return nil, domainMessage(err, msgNotFound, budget.ErrNotFound, payment.ErrNotFound)
	case http.StatusConflict:
		return nil, domainMessage(err, msgConflict, payment.ErrInvalidTransition)
	case http.StatusUnprocessableEntity:
		return nil, msgInvalidData
	default:
		return nil, msgInternal
	}
}

// domainMessage returns the message of the first domain sentinel that err matches,
// or fallback when none matches (infrastructure errors).
func domainMessage(err error, fallback string, sentinels ...error) string {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return s.Error()
		}
	}
	return fallback
}

// validationErrors are the input-validation sentinels: they answer 400 and their
// messages go to errors[]. JSON decoding errors and the domain validation errors
// (budget and billing payload) share the list.
var validationErrors = []error{
	jsonv2.ErrJSONSyntax,
	jsonv2.ErrJSONType,
	jsonv2.ErrJSONUnexpectedEOF,
	jsonv2.ErrJSONEmptyBody,
	jsonv2.ErrWrongPayloadFormat,
	budget.ErrCustomerRequired,
	budget.ErrNoWorks,
	budget.ErrNegativePrice,
	budget.ErrInvalidQuantity,
	billing.ErrInvalidPayload,
}

func isValidationError(err error) bool {
	return len(validationMessages(err)) > 0
}

// validationMessages returns the messages of the validation sentinels that err matches.
// Other causes wrapped in the same error are never included.
func validationMessages(err error) []string {
	var msgs []string
	for _, s := range validationErrors {
		if errors.Is(err, s) {
			msgs = append(msgs, s.Error())
		}
	}
	return msgs
}

func isInternalServerError(err error) bool {
	return errors.Is(err, jsonv2.ErrJSONUnsupportedValue) ||
		errors.Is(err, jsonv2.ErrJSONUnsupportedType) ||
		errors.Is(err, jsonv2.ErrJSONInvalidUnmarshal)
}

func isNotFoundError(err error) bool {
	return errors.Is(err, budget.ErrNotFound) ||
		errors.Is(err, payment.ErrNotFound)
}

func isConflictError(err error) bool {
	return errors.Is(err, app.ErrDataConflict) ||
		errors.Is(err, app.ErrInfraConflict) ||
		errors.Is(err, payment.ErrInvalidTransition)
}

func isUnprocessableEntityError(err error) bool {
	return errors.Is(err, app.ErrDataViolation)
}
