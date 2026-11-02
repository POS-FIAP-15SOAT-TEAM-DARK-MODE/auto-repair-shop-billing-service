package messaging

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	sagaMsgID  = "11111111-2222-4333-8444-555555555555"
	sagaID     = "saga-001"
	soID       = "so-001"
	requestID  = "req-001"
	fullBody   = `{"message_id":"` + sagaMsgID + `","saga_id":"` + sagaID + `","type":"CancelBudget","service_order_id":"` + soID + `","request_id":"` + requestID + `","payload":{}}`
	plainEvent = `{"message_id":"66666666-7777-4888-8999-000000000000","type":"CancelBudget","payload":{}}`
)

// captureLogs replaces the global logger with an observer for the duration of the test.
func captureLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	restore := logger.ReplaceGlobal(zap.New(core))
	t.Cleanup(restore)
	return logs
}

func entryWithEvent(t *testing.T, logs *observer.ObservedLogs, event string) observer.LoggedEntry {
	t.Helper()
	for _, e := range logs.All() {
		for _, f := range e.Context {
			if f.Key == "event" && f.String == event {
				return e
			}
		}
	}
	t.Fatalf("no log entry with event %q", event)
	return observer.LoggedEntry{}
}

func contextValue(e observer.LoggedEntry, key string) (string, bool) {
	for _, f := range e.Context {
		if f.Key == key {
			return f.String, true
		}
	}
	return "", false
}

func TestEnvelope_LogFieldsOmitMissingIDs(t *testing.T) {
	full, err := Decode([]byte(fullBody))
	require.NoError(t, err)
	assert.Len(t, full.logFields(), 5, "message_id, type, request_id, saga_id, service_order_id")

	plain, err := Decode([]byte(plainEvent))
	require.NoError(t, err)
	keys := map[string]bool{}
	for _, f := range plain.logFields() {
		keys[f.Key] = true
	}
	assert.True(t, keys["message_id"])
	assert.False(t, keys["saga_id"])
	assert.False(t, keys["service_order_id"])
	assert.False(t, keys["request_id"])
}

func TestConsumer_Handle_LogContextCarriesMessageIDs(t *testing.T) {
	logs := captureLogs(t)
	db, mock := newConsumerDB(t)
	c := NewConsumer(db, &fakeQueue{}, "u")
	c.RegisterHandler("CancelBudget", func(ctx context.Context, _ *sql.Tx, _ Envelope) error {
		logger.Of(ctx).Info("handler ran", zap.String("event", "budget.cancelled"))
		return nil
	})

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, c.Handle(context.Background(), Message{Body: fullBody, ReceiptHandle: "r"}))

	e := entryWithEvent(t, logs, "budget.cancelled")
	for key, want := range map[string]string{
		"message_id":       sagaMsgID,
		"saga_id":          sagaID,
		"service_order_id": soID,
		"request_id":       requestID,
		"type":             "CancelBudget",
	} {
		got, ok := contextValue(e, key)
		assert.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}
}

func TestConsumer_Handle_LogsDuplicateAndUnknownEvents(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		logs := captureLogs(t)
		db, mock := newConsumerDB(t)
		c := NewConsumer(db, &fakeQueue{}, "u")
		c.RegisterHandler("CancelBudget", func(context.Context, *sql.Tx, Envelope) error { return nil })
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectRollback()

		require.NoError(t, c.Handle(context.Background(), Message{Body: fullBody, ReceiptHandle: "r"}))
		e := entryWithEvent(t, logs, "message.duplicate_ignored")
		got, _ := contextValue(e, "saga_id")
		assert.Equal(t, sagaID, got)
	})

	t.Run("unknown type", func(t *testing.T) {
		logs := captureLogs(t)
		db, _ := newConsumerDB(t)
		body := `{"message_id":"` + sagaMsgID + `","type":"Mystery","payload":{}}`
		require.NoError(t, NewConsumer(db, &fakeQueue{}, "u").Handle(context.Background(), Message{Body: body, ReceiptHandle: "r"}))
		e := entryWithEvent(t, logs, "message.unknown_type_discarded")
		assert.Equal(t, zapcore.WarnLevel, e.Level)
	})

	t.Run("invalid body", func(t *testing.T) {
		logs := captureLogs(t)
		db, _ := newConsumerDB(t)
		err := NewConsumer(db, &fakeQueue{}, "u").Handle(context.Background(), Message{Body: "{", ReceiptHandle: "r"})
		assert.ErrorIs(t, err, ErrInvalidEnvelope)
		e := entryWithEvent(t, logs, "message.invalid")
		assert.Equal(t, zapcore.ErrorLevel, e.Level)
	})
}

func TestConsumer_Handle_FailureIsLoggedWithMessageIDs(t *testing.T) {
	logs := captureLogs(t)
	db, mock := newConsumerDB(t)
	c := NewConsumer(db, &fakeQueue{}, "u")
	c.RegisterHandler("CancelBudget", func(context.Context, *sql.Tx, Envelope) error { return errors.New("boom") })
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	require.Error(t, c.Handle(context.Background(), Message{Body: fullBody, ReceiptHandle: "r"}))

	require.Equal(t, 1, logs.Len())
	e := logs.All()[0]
	assert.Equal(t, zapcore.ErrorLevel, e.Level)
	component, _ := contextValue(e, "component")
	assert.Equal(t, "sqs-consumer", component)
	saga, _ := contextValue(e, "saga_id")
	assert.Equal(t, sagaID, saga)
}

func TestRelay_RunOnce_LogsPublishedMessagesAfterCommit(t *testing.T) {
	logs := captureLogs(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	relay := NewRelay(db, &fakePublisher{}, map[string]string{"billing-events": "arn:events"})
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).
			AddRow(1, "billing-events", nil, "CancelBudget", fullBody))
	mock.ExpectExec("UPDATE outbox SET published_at").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	e := entryWithEvent(t, logs, "message.published")
	saga, _ := contextValue(e, "saga_id")
	assert.Equal(t, sagaID, saga)
	msgID, _ := contextValue(e, "message_id")
	assert.Equal(t, sagaMsgID, msgID)
}

func TestRelay_RunOnce_NoPublishedLogWhenCommitFails(t *testing.T) {
	logs := captureLogs(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	relay := NewRelay(db, &fakePublisher{}, map[string]string{"billing-events": "arn:events"})
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).AddRow(1, "billing-events", nil, "CancelBudget", fullBody))
	mock.ExpectExec("UPDATE outbox SET published_at").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit boom"))

	_, err = relay.RunOnce(context.Background())
	assert.EqualError(t, err, "commit boom")
	for _, e := range logs.All() {
		assert.NotEqual(t, "message published", e.Message)
	}
}

func outboxPendingValue(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.OutboxPending.Write(&m))
	return m.GetGauge().GetValue()
}

func TestRelay_ObservePending_SetsGauge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM outbox WHERE published_at IS NULL").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	NewRelay(db, &fakePublisher{}, nil).observePending(context.Background())
	assert.Equal(t, float64(7), outboxPendingValue(t))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRelay_ObservePending_ErrorKeepsGauge(t *testing.T) {
	logs := captureLogs(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT count").WillReturnError(errors.New("db down"))
	before := outboxPendingValue(t)

	NewRelay(db, &fakePublisher{}, nil).observePending(context.Background())
	assert.Equal(t, before, outboxPendingValue(t))
	assert.Equal(t, 1, logs.FilterMessageSnippet("db down").Len())
}

func TestRelay_ObservePending_CancelledContextIsNotLogged(t *testing.T) {
	logs := captureLogs(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mock.ExpectQuery("SELECT count").WillReturnError(context.Canceled)

	NewRelay(db, &fakePublisher{}, nil).observePending(ctx)
	assert.Equal(t, 0, logs.Len())
}
