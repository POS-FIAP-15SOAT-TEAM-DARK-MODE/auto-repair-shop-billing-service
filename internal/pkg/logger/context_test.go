package logger_test

import (
	"context"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// captureLogs swaps the global logger for an observer and returns the recorded entries.
func captureLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	restore := logger.ReplaceGlobal(zap.New(core))
	t.Cleanup(restore)
	return logs
}

func fieldsOf(e observer.LoggedEntry) map[string]any {
	out := map[string]any{}
	for _, f := range e.Context {
		switch f.Type {
		case zapcore.StringType:
			out[f.Key] = f.String
		default:
			out[f.Key] = f.Interface
		}
	}
	return out
}

func TestRequest_TagsLinesWithRequestID(t *testing.T) {
	logs := captureLogs(t)

	ctx, reqID := logger.Request(context.Background(), "req-42")
	logger.Of(ctx).Info("request.started")

	require.Equal(t, "req-42", reqID)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "req-42", fieldsOf(logs.All()[0])["request_id"])
}

func TestWith_AddsFieldsToEveryLineOfTheContext(t *testing.T) {
	logs := captureLogs(t)

	ctx := logger.With(context.Background(),
		zap.String("message_id", "m-1"),
		zap.String("saga_id", "s-1"),
		zap.String("service_order_id", "so-1"),
	)
	logger.Of(ctx).Info("payment.approved")
	logger.Of(ctx).Warn("payment.request_discarded")

	require.Equal(t, 2, logs.Len())
	for _, e := range logs.All() {
		f := fieldsOf(e)
		assert.Equal(t, "m-1", f["message_id"])
		assert.Equal(t, "s-1", f["saga_id"])
		assert.Equal(t, "so-1", f["service_order_id"])
	}
}

func TestWith_RedactsSensitiveFields(t *testing.T) {
	logs := captureLogs(t)

	ctx := logger.With(context.Background(), zap.String("jwt", "eyJ.abc.def"), zap.String("cpf", "12345678901"))
	logger.Of(ctx).Info("ok", zap.String("authorization", "Bearer x"), zap.String("access_token", "t"))

	require.Equal(t, 1, logs.Len())
	f := fieldsOf(logs.All()[0])
	assert.Equal(t, "[REDACTED]", f["jwt"])
	assert.Equal(t, "[REDACTED]", f["cpf"])
	assert.Equal(t, "[REDACTED]", f["authorization"])
	assert.Equal(t, "[REDACTED]", f["access_token"])
}

func TestWith_WithoutLoggerInContextUsesGlobal(t *testing.T) {
	logs := captureLogs(t)

	ctx := logger.With(context.TODO(), zap.String("saga_id", "s-9"))
	logger.Of(ctx).Info("saga.step")

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "s-9", fieldsOf(logs.All()[0])["saga_id"])
}
