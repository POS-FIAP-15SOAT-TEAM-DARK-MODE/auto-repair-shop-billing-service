package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePublisher struct {
	fail  map[string]bool
	calls []string
	attrs []map[string]string
}

func (f *fakePublisher) Publish(_ context.Context, topicARN, body string, attrs map[string]string) error {
	f.calls = append(f.calls, topicARN+"|"+body)
	f.attrs = append(f.attrs, attrs)
	if f.fail[body] {
		return errors.New("sns down")
	}
	return nil
}

var pendingCols = []string{"id", "topic", "target", "type", "body"}

func TestRelay_RunOnce_PublishesAndMarks(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &fakePublisher{}
	relay := NewRelay(db, pub, map[string]string{"billing-events": "arn:events"})

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).
			AddRow(1, "billing-events", nil, "BudgetCancelled", `{"a":1}`).
			AddRow(2, "billing-events", "billing", "PaymentFailed", `{"b":2}`))
	mock.ExpectExec("UPDATE outbox SET published_at").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE outbox SET published_at").WithArgs(int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{`arn:events|{"a":1}`, `arn:events|{"b":2}`}, pub.calls)
	assert.Equal(t, map[string]string{"type": "PaymentFailed", "target": "billing"}, pub.attrs[1])
	assert.Equal(t, map[string]string{"type": "BudgetCancelled"}, pub.attrs[0])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRelay_RunOnce_PublishFailureKeepsRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &fakePublisher{fail: map[string]bool{`{"a":1}`: true}}
	relay := NewRelay(db, pub, map[string]string{"billing-events": "arn:events"})

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).
			AddRow(1, "billing-events", nil, "BudgetCancelled", `{"a":1}`).
			AddRow(2, "billing-events", nil, "BudgetCancelled", `{"b":2}`))
	// row 1 is NOT marked: publish failed
	mock.ExpectExec("UPDATE outbox SET published_at").WithArgs(int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := relay.RunOnce(context.Background())
	assert.Equal(t, 1, n)
	assert.ErrorContains(t, err, "outbox row 1: publish")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRelay_RunOnce_UnknownTopicIsNotPublished(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &fakePublisher{}
	relay := NewRelay(db, pub, map[string]string{})

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).AddRow(1, "unknown", nil, "T", `{}`))
	mock.ExpectCommit()

	n, err := relay.RunOnce(context.Background())
	assert.Equal(t, 0, n)
	assert.ErrorContains(t, err, `no ARN configured for topic "unknown"`)
	assert.Empty(t, pub.calls)
}

func TestRelay_RunOnce_DBErrors(t *testing.T) {
	t.Run("begin fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectBegin().WillReturnError(errors.New("no conn"))
		_, err := NewRelay(db, &fakePublisher{}, nil).RunOnce(context.Background())
		assert.EqualError(t, err, "no conn")
	})

	t.Run("query fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery("FOR UPDATE").WillReturnError(errors.New("query boom"))
		_, err := NewRelay(db, &fakePublisher{}, nil).RunOnce(context.Background())
		assert.EqualError(t, err, "query boom")
	})

	t.Run("scan fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("not-a-number"))
		_, err := NewRelay(db, &fakePublisher{}, nil).RunOnce(context.Background())
		assert.Error(t, err)
	})

	t.Run("mark fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows(pendingCols).AddRow(1, "t", nil, "T", `{}`))
		mock.ExpectExec("UPDATE outbox").WillReturnError(errors.New("update boom"))
		relay := NewRelay(db, &fakePublisher{}, map[string]string{"t": "arn"})
		_, err := relay.RunOnce(context.Background())
		assert.EqualError(t, err, "update boom")
	})

	t.Run("commit fails", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows(pendingCols))
		mock.ExpectCommit().WillReturnError(errors.New("commit boom"))
		_, err := NewRelay(db, &fakePublisher{}, nil).RunOnce(context.Background())
		assert.EqualError(t, err, "commit boom")
	})
}

func TestRelay_Run_StopsOnCancel(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, NewRelay(db, &fakePublisher{}, nil).Run(ctx))
}
