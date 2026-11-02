package messaging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
	"go.uber.org/zap"
)

const (
	selectPendingQuery = `SELECT id, topic, target, type, body FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`
	markPublishedQuery = `UPDATE outbox SET published_at = now() WHERE id = $1`
	countPendingQuery  = `SELECT count(*) FROM outbox WHERE published_at IS NULL`
)

// Relay publishes outbox rows to SNS. A row is marked as published only after a successful Publish.
type Relay struct {
	db        *sql.DB
	publisher Publisher
	topics    map[string]string // logical topic -> topic ARN
	interval  time.Duration
	batchSize int
}

// NewRelay returns a relay that polls every second and publishes up to 20 rows per tick.
func NewRelay(db *sql.DB, publisher Publisher, topics map[string]string) *Relay {
	return &Relay{db: db, publisher: publisher, topics: topics, interval: time.Second, batchSize: 20}
}

// Run calls RunOnce every interval until ctx is cancelled, and refreshes the outbox_pending gauge on each tick.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if _, err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			logger.Global().Error(err, zap.String("component", "outbox-relay"))
		}
		r.observePending(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// observePending sets outbox_pending to the number of rows still waiting to be published.
func (r *Relay) observePending(ctx context.Context) {
	var pending int64
	if err := r.db.QueryRowContext(ctx, countPendingQuery).Scan(&pending); err != nil {
		if ctx.Err() == nil {
			logger.Global().Error(err, zap.String("component", "outbox-relay"))
		}
		return
	}
	metrics.OutboxPending.Set(float64(pending))
}

type pendingRow struct {
	id     int64
	topic  string
	target sql.NullString
	typ    string
	body   string
}

// RunOnce publishes one batch and returns how many messages were published.
// Failed rows stay unpublished and are retried on the next run.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	batch, err := r.selectPending(ctx, tx)
	if err != nil {
		return 0, err
	}

	published := 0
	var sent []Envelope
	var errs []error
	for _, row := range batch {
		topicARN, ok := r.topics[row.topic]
		if !ok {
			errs = append(errs, fmt.Errorf("outbox row %d: no ARN configured for topic %q", row.id, row.topic))
			continue
		}

		attrs := map[string]string{"type": row.typ}
		if row.target.Valid {
			attrs["target"] = row.target.String
		}
		if err := r.publisher.Publish(ctx, topicARN, row.body, attrs); err != nil {
			errs = append(errs, fmt.Errorf("outbox row %d: publish: %w", row.id, err))
			continue
		}

		if _, err := tx.ExecContext(ctx, markPublishedQuery, row.id); err != nil {
			return published, err
		}
		published++
		if env, err := Decode([]byte(row.body)); err == nil {
			sent = append(sent, env)
		}
	}

	if err := tx.Commit(); err != nil {
		return published, err
	}
	// Logged after the commit: a row is only "published" once it is marked as such.
	for _, env := range sent {
		logger.Of(ctx).Info("message published", append(env.logFields(), zap.String("event", "message.published"))...)
	}
	return published, errors.Join(errs...)
}

func (r *Relay) selectPending(ctx context.Context, tx *sql.Tx) ([]pendingRow, error) {
	rows, err := tx.QueryContext(ctx, selectPendingQuery, r.batchSize)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var batch []pendingRow
	for rows.Next() {
		var p pendingRow
		if err := rows.Scan(&p.id, &p.topic, &p.target, &p.typ, &p.body); err != nil {
			return nil, err
		}
		batch = append(batch, p)
	}
	return batch, rows.Err()
}
