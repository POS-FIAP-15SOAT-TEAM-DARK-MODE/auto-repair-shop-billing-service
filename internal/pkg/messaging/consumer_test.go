package messaging

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validBody = `{"message_id":"b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11","type":"CancelBudget","payload":{}}`

type fakeQueue struct {
	batches   [][]Message
	recvErr   error
	deleted   []string
	deleteErr error
	onEmpty   func()
}

func (f *fakeQueue) ReceiveMessage(_ context.Context, _ string, _, _ int32) ([]Message, error) {
	if len(f.batches) == 0 {
		if f.onEmpty != nil {
			f.onEmpty()
		}
		return nil, nil
	}
	if f.recvErr != nil {
		err := f.recvErr
		f.recvErr = nil
		return nil, err
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}

func (f *fakeQueue) DeleteMessage(_ context.Context, _, receipt string) error {
	f.deleted = append(f.deleted, receipt)
	return f.deleteErr
}

func newConsumerDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func TestConsumer_Handle_AppliesOnceAndDeletes(t *testing.T) {
	db, mock := newConsumerDB(t)
	q := &fakeQueue{}
	c := NewConsumer(db, q, "http://queue")

	called := false
	c.RegisterHandler("CancelBudget", func(_ context.Context, tx *sql.Tx, env Envelope) error {
		called = true
		assert.Equal(t, "b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11", env.MessageID)
		_, err := tx.ExecContext(context.Background(), "UPDATE budget")
		return err
	})

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WithArgs("b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE budget").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, c.Handle(context.Background(), Message{Body: validBody, ReceiptHandle: "r1"}))
	assert.True(t, called)
	assert.Equal(t, []string{"r1"}, q.deleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConsumer_Handle_DuplicateIsNotApplied(t *testing.T) {
	db, mock := newConsumerDB(t)
	q := &fakeQueue{}
	c := NewConsumer(db, q, "u")
	c.RegisterHandler("CancelBudget", func(context.Context, *sql.Tx, Envelope) error {
		t.Fatal("handler must not run for a duplicate")
		return nil
	})

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	require.NoError(t, c.Handle(context.Background(), Message{Body: validBody, ReceiptHandle: "r2"}))
	assert.Equal(t, []string{"r2"}, q.deleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConsumer_Handle_UnknownTypeIsDiscarded(t *testing.T) {
	db, mock := newConsumerDB(t)
	q := &fakeQueue{}
	c := NewConsumer(db, q, "u")

	body := `{"message_id":"b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11","type":"Unknown","payload":{}}`
	require.NoError(t, c.Handle(context.Background(), Message{Body: body, ReceiptHandle: "r3"}))
	assert.Equal(t, []string{"r3"}, q.deleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConsumer_Handle_InvalidBodyIsNotDeleted(t *testing.T) {
	db, _ := newConsumerDB(t)
	q := &fakeQueue{}
	err := NewConsumer(db, q, "u").Handle(context.Background(), Message{Body: "{", ReceiptHandle: "r4"})
	assert.ErrorIs(t, err, ErrInvalidEnvelope)
	assert.Empty(t, q.deleted)
}

func TestConsumer_Handle_ErrorsKeepMessage(t *testing.T) {
	handlerErr := errors.New("handler boom")

	tests := map[string]struct {
		setup   func(sqlmock.Sqlmock)
		handler Handler
		wantErr string
	}{
		"begin fails": {
			setup:   func(m sqlmock.Sqlmock) { m.ExpectBegin().WillReturnError(errors.New("no tx")) },
			wantErr: "no tx",
		},
		"dedupe insert fails": {
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectBegin()
				m.ExpectExec("INSERT INTO processed_message").WillReturnError(errors.New("insert boom"))
				m.ExpectRollback()
			},
			wantErr: "insert boom",
		},
		"rows affected fails": {
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectBegin()
				m.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewErrorResult(errors.New("rows boom")))
				m.ExpectRollback()
			},
			wantErr: "rows boom",
		},
		"handler fails rolls back": {
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectBegin()
				m.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectRollback()
			},
			handler: func(context.Context, *sql.Tx, Envelope) error { return handlerErr },
			wantErr: "handle CancelBudget: handler boom",
		},
		"commit fails": {
			setup: func(m sqlmock.Sqlmock) {
				m.ExpectBegin()
				m.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit().WillReturnError(errors.New("commit boom"))
			},
			handler: func(context.Context, *sql.Tx, Envelope) error { return nil },
			wantErr: "commit boom",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			db, mock := newConsumerDB(t)
			q := &fakeQueue{}
			c := NewConsumer(db, q, "u")
			h := tc.handler
			if h == nil {
				h = func(context.Context, *sql.Tx, Envelope) error { return nil }
			}
			c.RegisterHandler("CancelBudget", h)
			tc.setup(mock)

			err := c.Handle(context.Background(), Message{Body: validBody, ReceiptHandle: "rx"})
			assert.ErrorContains(t, err, tc.wantErr)
			assert.Empty(t, q.deleted, "message must stay in the queue")
		})
	}
}

func TestConsumer_Handle_DeleteFailureIsReturned(t *testing.T) {
	db, mock := newConsumerDB(t)
	q := &fakeQueue{deleteErr: errors.New("delete boom")}
	c := NewConsumer(db, q, "u")
	c.RegisterHandler("CancelBudget", func(context.Context, *sql.Tx, Envelope) error { return nil })

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	assert.EqualError(t, c.Handle(context.Background(), Message{Body: validBody}), "delete boom")
}

func TestConsumer_Run_ProcessesThenStops(t *testing.T) {
	db, mock := newConsumerDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := &fakeQueue{
		batches: [][]Message{{{Body: "{", ReceiptHandle: "bad"}, {Body: `{"message_id":"b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11","type":"X"}`, ReceiptHandle: "unknown"}}},
		onEmpty: cancel,
	}
	c := NewConsumer(db, q, "u")
	require.NoError(t, c.Run(ctx))
	assert.Equal(t, []string{"unknown"}, q.deleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConsumer_Run_ReceiveErrorThenStop(t *testing.T) {
	db, _ := newConsumerDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := &fakeQueue{recvErr: errors.New("sqs down"), onEmpty: cancel}
	q.batches = [][]Message{nil}
	c := NewConsumer(db, q, "u")
	require.NoError(t, c.Run(ctx))
}

func TestConsumer_Run_CancelledBeforeStart(t *testing.T) {
	db, _ := newConsumerDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, NewConsumer(db, &fakeQueue{}, "u").Run(ctx))
}
