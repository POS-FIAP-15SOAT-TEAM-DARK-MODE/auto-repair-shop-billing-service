package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnqueue(t *testing.T) {
	env := Envelope{MessageID: "b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11", Type: "BudgetCancelled", Payload: []byte(`{}`)}

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(env.MessageID, "billing-events", nil, "BudgetCancelled", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, Enqueue(context.Background(), tx, "billing-events", "", env))
	require.NoError(t, tx.Commit())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEnqueue_WithTargetAndError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(sqlmock.AnyArg(), "saga-commands", "billing", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("boom"))

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	err = Enqueue(context.Background(), tx, "saga-commands", "billing", Envelope{MessageID: "m", Type: "T", Payload: []byte(`{}`)})
	assert.EqualError(t, err, "boom")
}

func TestEnqueue_MarshalError(t *testing.T) {
	err := Enqueue(context.Background(), nil, "t", "", Envelope{Payload: []byte("{")})
	assert.Error(t, err)
}
