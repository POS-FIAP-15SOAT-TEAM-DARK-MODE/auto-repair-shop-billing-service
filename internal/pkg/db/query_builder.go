package db

import (
	"fmt"
	"strings"

	"github.com/lib/pq"
)

type queryBuilder struct {
	baseQuery      string
	counter        int64
	conditions     []string
	args           []any
	groupByFields  []string
	limit          int64
	offset         int64
	orderField     string
	orderDirection OrderDirection
	rawOrderExpr   string
}

type OrderDirection string

const (
	ASC  OrderDirection = "ASC"
	DESC OrderDirection = "DESC"
)

func QueryBuilder(base string) *queryBuilder {
	return &queryBuilder{
		baseQuery:  base,
		counter:    0,
		conditions: make([]string, 0),
		args:       make([]any, 0),
		limit:      0,
		offset:     0,
	}
}

func (q *queryBuilder) Add(condition string, value any) *queryBuilder {
	if value == nil {
		return q
	}

	if v, ok := value.(string); ok && v == "" {
		return q
	}

	q.counter++
	q.conditions = append(q.conditions, fmt.Sprintf("%s $%d", condition, q.counter))
	q.args = append(q.args, value)
	return q
}

// AddAny adds "condition = ANY($N)" to the WHERE clause using a PostgreSQL array parameter.
// Skipped when values is empty — safe to call unconditionally for optional filters.
func (q *queryBuilder) AddAny(condition string, values []string) *queryBuilder {
	if len(values) == 0 {
		return q
	}
	q.counter++
	q.conditions = append(q.conditions, fmt.Sprintf("%s = ANY($%d)", condition, q.counter))
	q.args = append(q.args, pq.Array(values))
	return q
}

// GroupBy adds a GROUP BY clause with the given fields, emitted after WHERE and before ORDER BY.
func (q *queryBuilder) GroupBy(fields ...string) *queryBuilder {
	q.groupByFields = append(q.groupByFields, fields...)
	return q
}

func (q *queryBuilder) AddPagination(limit, offset int64) *queryBuilder {
	q.limit = limit
	q.offset = offset
	return q
}

func (q *queryBuilder) OrderBy(field string, direction OrderDirection) *queryBuilder {
	q.orderDirection = direction
	q.orderField = field
	return q
}

// AddNotIn adds "field NOT IN ($N)" to the WHERE clause using a PostgreSQL array parameter.
// Skipped when values is empty — safe to call unconditionally for optional filters.
func (q *queryBuilder) AddNotIn(field string, values []string) *queryBuilder {
	if len(values) == 0 {
		return q
	}
	q.counter++
	q.conditions = append(q.conditions, fmt.Sprintf("%s != ALL($%d)", field, q.counter))
	q.args = append(q.args, pq.Array(values))
	return q
}

// OrderByRaw sets a raw ORDER BY expression (e.g. a CASE statement).
// When set, this takes precedence over OrderBy.
func (q *queryBuilder) OrderByRaw(expr string) *queryBuilder {
	q.rawOrderExpr = expr
	return q
}

func (q *queryBuilder) Build() (string, []any) {
	var sb strings.Builder
	sb.WriteString(q.baseQuery)

	if len(q.conditions) > 0 {
		sb.WriteString(" WHERE ")
		for i, cond := range q.conditions {
			if i > 0 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(cond)
		}
	}

	if len(q.groupByFields) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(q.groupByFields, ", "))
	}

	if q.rawOrderExpr != "" {
		sb.WriteString(fmt.Sprintf(" ORDER BY %s", q.rawOrderExpr))
	} else if q.orderField != "" {
		if q.orderDirection == "" {
			q.orderDirection = ASC
		}

		sb.WriteString(fmt.Sprintf(" ORDER BY %s %s", q.orderField, q.orderDirection))
	}

	if q.limit > 0 {
		q.counter++
		sb.WriteString(fmt.Sprintf(" LIMIT $%d", q.counter))
		q.args = append(q.args, q.limit)
	}

	if q.offset > 0 {
		q.counter++
		sb.WriteString(fmt.Sprintf(" OFFSET $%d", q.counter))
		q.args = append(q.args, q.offset)
	}

	return sb.String(), q.args
}
