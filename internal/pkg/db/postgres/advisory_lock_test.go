package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lockRows(v bool) *sqlmock.Rows { return sqlmock.NewRows([]string{"locked"}).AddRow(v) }

func newLockDB(t *testing.T) (*AdvisoryLock, sqlmock.Sqlmock, func() int) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewAdvisoryLock(db, 7), mock, func() int { return db.Stats().OpenConnections }
}

func TestAdvisoryLock_TakesAndReleasesOnTheSameConnectionAndKeepsItInThePool(t *testing.T) {
	lock, mock, open := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WithArgs(int64(7)).WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WithArgs(int64(7)).WillReturnRows(lockRows(true))

	lease, ok, err := lock.TryLock(context.Background())

	require.NoError(t, err)
	require.True(t, ok)
	lease.Release()
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, 1, open(), "a clean release gives the connection back to the pool")
}

func TestAdvisoryLock_AnotherReplicaHoldingItIsNotAnError(t *testing.T) {
	lock, mock, open := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(false))

	lease, ok, err := lock.TryLock(context.Background())

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, lease)
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Equal(t, 1, open(), "the connection goes back: it holds nothing")
}

func TestAdvisoryLock_AFailedTryDiscardsTheConnection(t *testing.T) {
	// The server may have granted the lock before the answer was lost; a connection that goes back to the
	// pool still holding it would make the next TryLock "succeed" (the lock is re-entrant) and one unlock
	// would not free it.
	lock, mock, open := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnError(errors.New("context deadline exceeded"))

	_, ok, err := lock.TryLock(context.Background())

	assert.EqualError(t, err, "context deadline exceeded")
	assert.False(t, ok)
	assert.Zero(t, open(), "the connection is closed, not returned to the pool")
}

func TestAdvisoryLock_ReleaseDiscardsTheConnectionWhenTheUnlockFails(t *testing.T) {
	lock, mock, open := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WillReturnError(errors.New("connection lost"))
	lease, ok, err := lock.TryLock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	assert.NotPanics(t, lease.Release, "a failed unlock must not break the worker")

	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Zero(t, open(), "the session is ended, and the lock with it")
}

func TestAdvisoryLock_ReleaseDiscardsTheConnectionWhenTheServerSaysItWasNotHeld(t *testing.T) {
	lock, mock, open := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WillReturnRows(lockRows(false))
	lease, _, err := lock.TryLock(context.Background())
	require.NoError(t, err)

	lease.Release()

	assert.Zero(t, open())
}

func TestAdvisoryLock_ReleaseWorksAfterTheContextIsCancelled(t *testing.T) {
	lock, mock, _ := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WillReturnRows(lockRows(true))
	ctx, cancel := context.WithCancel(context.Background())

	lease, ok, err := lock.TryLock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	cancel() // the service is shutting down

	lease.Release()
	assert.NoError(t, mock.ExpectationsWereMet(), "the unlock does not depend on the cancelled context")
}

func TestAdvisoryLock_HeldSaysWhetherTheSessionIsStillAlive(t *testing.T) {
	lock, mock, _ := newLockDB(t)
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(true))
	mock.ExpectPing()
	mock.ExpectPing().WillReturnError(errors.New("connection reset"))
	lease, _, err := lock.TryLock(context.Background())
	require.NoError(t, err)

	assert.True(t, lease.Held(context.Background()))
	assert.False(t, lease.Held(context.Background()), "a dead connection means the lock is gone")
}
