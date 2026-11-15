package db_test

import (
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db"
	"github.com/stretchr/testify/assert"
)

func TestQueryBuilder_Base(t *testing.T) {
	qb := db.QueryBuilder("SELECT * FROM table")
	query, args := qb.Build()

	assert.Equal(t, "SELECT * FROM table", query)
	assert.Empty(t, args)
}

func TestQueryBuilder_Conditions(t *testing.T) {
	qb := db.QueryBuilder("SELECT * FROM table")
	qb.Add("name =", "John")
	qb.Add("age >", 20)
	qb.Add("empty", "") // Should be ignored
	qb.Add("nil", nil)  // Should be ignored
	query, args := qb.Build()

	assert.Equal(t, "SELECT * FROM table WHERE name = $1 AND age > $2", query)
	assert.Equal(t, []any{"John", 20}, args)
}

func TestQueryBuilder_Pagination(t *testing.T) {
	qb := db.QueryBuilder("SELECT * FROM table")
	qb.AddPagination(10, 20)
	query, args := qb.Build()

	assert.Equal(t, "SELECT * FROM table LIMIT $1 OFFSET $2", query)
	assert.Equal(t, []any{int64(10), int64(20)}, args)
}

func TestQueryBuilder_OrderBy(t *testing.T) {
	t.Run("DefaultASC", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT * FROM table")
		qb.OrderBy("created_at", "")
		query, _ := qb.Build()
		assert.Equal(t, "SELECT * FROM table ORDER BY created_at ASC", query)
	})

	t.Run("DESC", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT * FROM table")
		qb.OrderBy("id", db.DESC)
		query, _ := qb.Build()
		assert.Equal(t, "SELECT * FROM table ORDER BY id DESC", query)
	})
}

func TestQueryBuilder_AddAny(t *testing.T) {
	t.Run("skipped when empty slice", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT * FROM t")
		qb.AddAny("status", []string{})
		query, args := qb.Build()
		assert.Equal(t, "SELECT * FROM t", query)
		assert.Empty(t, args)
	})

	t.Run("adds ANY condition", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT * FROM t")
		qb.AddAny("status", []string{"NEW", "ACTIVE"})
		query, args := qb.Build()
		assert.Equal(t, "SELECT * FROM t WHERE status = ANY($1)", query)
		assert.Len(t, args, 1)
	})

	t.Run("combined with Add and pagination", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT * FROM t")
		qb.Add("name =", "John")
		qb.AddAny("role", []string{"ADMIN", "MECHANIC"})
		qb.AddPagination(5, 0)
		query, args := qb.Build()
		assert.Equal(t, "SELECT * FROM t WHERE name = $1 AND role = ANY($2) LIMIT $3", query)
		assert.Len(t, args, 3)
	})
}

func TestQueryBuilder_GroupBy(t *testing.T) {
	t.Run("single field", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT status, COUNT(*) FROM t")
		qb.GroupBy("status")
		query, _ := qb.Build()
		assert.Equal(t, "SELECT status, COUNT(*) FROM t GROUP BY status", query)
	})

	t.Run("multiple fields", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT a, b, COUNT(*) FROM t")
		qb.GroupBy("a", "b")
		query, _ := qb.Build()
		assert.Equal(t, "SELECT a, b, COUNT(*) FROM t GROUP BY a, b", query)
	})

	t.Run("group by with where and order by", func(t *testing.T) {
		qb := db.QueryBuilder("SELECT status, COUNT(*) FROM t")
		qb.Add("active =", true)
		qb.GroupBy("status")
		qb.OrderBy("status", db.ASC)
		query, args := qb.Build()
		assert.Equal(t, "SELECT status, COUNT(*) FROM t WHERE active = $1 GROUP BY status ORDER BY status ASC", query)
		assert.Equal(t, []any{true}, args)
	})
}

func TestQueryBuilder_Complex(t *testing.T) {
	qb := db.QueryBuilder("SELECT * FROM table")
	qb.Add("status =", "ACTIVE")
	qb.OrderBy("id", db.ASC)
	qb.AddPagination(10, 0) // Offset 0 should not be added
	query, args := qb.Build()

	assert.Equal(t, "SELECT * FROM table WHERE status = $1 ORDER BY id ASC LIMIT $2", query)
	assert.Equal(t, []any{"ACTIVE", int64(10)}, args)
}
