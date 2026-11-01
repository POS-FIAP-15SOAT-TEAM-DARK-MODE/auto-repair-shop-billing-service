package messaging

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"go.uber.org/zap"
)

const insertProcessedQuery = `INSERT INTO processed_message (message_id) VALUES ($1) ON CONFLICT (message_id) DO NOTHING`

// Handler applies a message inside the consumer's transaction (same tx as the dedupe row).
type Handler func(ctx context.Context, tx *sql.Tx, env Envelope) error

// Consumer receives messages from SQS and applies them exactly once per message_id.
//
// Per message: dedupe insert + handler + commit, then DeleteMessage. Any error leaves the
// message in the queue (SQS redelivers; after maxReceiveCount it goes to the DLQ).
type Consumer struct {
	db       *sql.DB
	queue    Queue
	queueURL string
	handlers map[string]Handler
}

func NewConsumer(db *sql.DB, queue Queue, queueURL string) *Consumer {
	return &Consumer{db: db, queue: queue, queueURL: queueURL, handlers: map[string]Handler{}}
}

// RegisterHandler sets the handler for a message type. Unknown types are discarded with a log.
func (c *Consumer) RegisterHandler(typ string, h Handler) {
	c.handlers[typ] = h
}

// Run long-polls the queue until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		msgs, err := c.queue.ReceiveMessage(ctx, c.queueURL, 10, 20)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			logger.Global().Error(err, zap.String("component", "sqs-consumer"))
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		for _, m := range msgs {
			if err := c.Handle(ctx, m); err != nil {
				logger.Global().Error(err, zap.String("component", "sqs-consumer"))
			}
		}
	}
	return nil
}

// Handle processes one message. A non-nil error means the message must stay in the queue.
func (c *Consumer) Handle(ctx context.Context, m Message) error {
	env, err := Decode([]byte(m.Body))
	if err != nil {
		return fmt.Errorf("decode message: %w", err)
	}

	h, ok := c.handlers[env.Type]
	if !ok {
		logger.Global().Warn("unknown message type, discarding", zap.String("type", env.Type), zap.String("message_id", env.MessageID))
		return c.delete(ctx, m)
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, insertProcessedQuery, env.MessageID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		logger.Global().Info("duplicate message ignored", zap.String("message_id", env.MessageID), zap.String("type", env.Type))
		return c.delete(ctx, m)
	}

	if err := h(ctx, tx, env); err != nil {
		return fmt.Errorf("handle %s: %w", env.Type, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return c.delete(ctx, m)
}

// delete runs after commit. If it fails the message is redelivered and the dedupe row catches it.
func (c *Consumer) delete(ctx context.Context, m Message) error {
	return c.queue.DeleteMessage(ctx, c.queueURL, m.ReceiptHandle)
}
