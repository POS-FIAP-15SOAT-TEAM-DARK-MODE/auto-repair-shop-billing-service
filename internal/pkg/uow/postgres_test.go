package uow

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/stretchr/testify/assert"
)

func TestNewTransactionalUoW(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	uowExec := NewTransactionalUoW(db)
	assert.NotNil(t, uowExec)

	t.Run("Success", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectCommit()

		err = uowExec.Execute(context.Background(), func(ctx context.Context) error {
			tx, found := txFrom(ctx)
			assert.True(t, found)
			assert.NotNil(t, tx)
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("Failure", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectRollback()

		expectedErr := errors.New("step failed")
		err = uowExec.Execute(context.Background(), func(ctx context.Context) error {
			return expectedErr
		})
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("BeginError", func(t *testing.T) {
		expectedErr := errors.New("begin error")
		mock.ExpectBegin().WillReturnError(expectedErr)

		err = uowExec.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), expectedErr.Error())
	})
}

func TestOnSuccess_MissingTransaction(t *testing.T) {
	err := onSuccess(context.Background())
	assert.ErrorIs(t, err, ErrMissingPostgresTransaction)
}

func TestOnSuccess_CommitError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(assert.AnError)

	tx, _ := db.Begin()
	ctx := withTx(context.Background(), tx)

	err := onSuccess(ctx)
	assert.Error(t, err)
}

func TestOnFailure_MissingTransaction(t *testing.T) {
	err := onFailure(context.Background(), assert.AnError)
	assert.ErrorIs(t, err, ErrMissingPostgresTransaction)
}

func TestOnFailure_RollbackError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectRollback().WillReturnError(assert.AnError)

	tx, _ := db.Begin()
	ctx := withTx(context.Background(), tx)

	err := onFailure(ctx, assert.AnError)
	assert.Error(t, err)
}

func TestGetOneTimeTransaction(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	app.ConnectWithDB(db)

	got, err := GetOneTimeTransaction(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, got)
}

func TestGetTransaction(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	t.Run("Found", func(t *testing.T) {
		mock.ExpectBegin()
		tx, _ := db.Begin()
		ctx := withTx(context.Background(), tx)

		got, err := GetTransaction(ctx)
		assert.NoError(t, err)
		assert.Equal(t, tx, got)
	})

	t.Run("NotFound", func(t *testing.T) {
		got, err := GetTransaction(context.Background())
		assert.ErrorIs(t, err, ErrMissingPostgresTransaction)
		assert.Nil(t, got)
	})
}
