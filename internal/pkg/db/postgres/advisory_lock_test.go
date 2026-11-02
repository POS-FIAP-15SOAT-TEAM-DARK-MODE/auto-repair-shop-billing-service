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

func TestAdvisoryLock_TakesAndReleasesOnTheSameConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("pg_try_advisory_lock").WithArgs(int64(7)).WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WithArgs(int64(7)).WillReturnRows(lockRows(true))

	release, ok, err := NewAdvisoryLock(db, 7).TryLock(context.Background())

	require.NoError(t, err)
	require.True(t, ok)
	release()
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAdvisoryLock_AnotherReplicaHoldingItIsNotAnError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(false))

	release, ok, err := NewAdvisoryLock(db, 7).TryLock(context.Background())

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, release)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAdvisoryLock_ReportsDatabaseErrors(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnError(errors.New("boom"))

	_, ok, err := NewAdvisoryLock(db, 7).TryLock(context.Background())

	assert.EqualError(t, err, "boom")
	assert.False(t, ok)
}

func TestAdvisoryLock_ReleaseDiscardsTheConnectionWhenTheUnlockFails(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WillReturnError(errors.New("connection lost"))

	release, ok, err := NewAdvisoryLock(db, 7).TryLock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	assert.NotPanics(t, release, "a failed unlock must not break the worker")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAdvisoryLock_ReleaseWorksAfterTheContextIsCancelled(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("pg_try_advisory_lock").WillReturnRows(lockRows(true))
	mock.ExpectQuery("pg_advisory_unlock").WillReturnRows(lockRows(true))
	ctx, cancel := context.WithCancel(context.Background())

	release, ok, err := NewAdvisoryLock(db, 7).TryLock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	cancel() // the service is shutting down

	release()
	assert.NoError(t, mock.ExpectationsWereMet(), "the unlock does not depend on the cancelled context")
}
