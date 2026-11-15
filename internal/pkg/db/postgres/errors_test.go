package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db/postgres"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

func TestError_Mapping(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{
			"UniqueViolation",
			&pq.Error{Code: "23505", Message: "duplicate key"},
			app.ErrDataConflict,
		},
		{
			"CheckViolation",
			&pq.Error{Code: "23514", Message: "check constraint"},
			app.ErrDataViolation,
		},
		{
			"ForeignKeyViolation",
			&pq.Error{Code: "23503", Message: "fk violation"},
			app.ErrDataViolation,
		},
		{
			"NotNullViolation",
			&pq.Error{Code: "23502", Message: "not null violation"},
			app.ErrDataViolation,
		},
		{
			"SerializationFailure",
			&pq.Error{Code: "40001", Message: "serialization failure"},
			app.ErrInfraConflict,
		},
		{
			"DeadlockDetected",
			&pq.Error{Code: "40P01", Message: "deadlock detected"},
			app.ErrInfraConflict,
		},
		{
			"NonPQError",
			errors.New("standard error"),
			errors.New("standard error"),
		},
		{
			"UnhandledPQError",
			&pq.Error{Code: "99999", Message: "unknown pq error"},
			&pq.Error{Code: "99999", Message: "unknown pq error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postgres.Error(ctx, tt.err)
			if tt.name == "NonPQError" || tt.name == "UnhandledPQError" {
				assert.Equal(t, tt.expected.Error(), got.Error())
			} else {
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}
