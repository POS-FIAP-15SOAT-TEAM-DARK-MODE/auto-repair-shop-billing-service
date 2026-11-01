package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
)

const insertOutboxQuery = `INSERT INTO outbox (message_id, topic, target, type, body) VALUES ($1, $2, $3, $4, $5)`

// Enqueue stores the envelope in the outbox inside the caller's transaction.
// topic is the logical topic (e.g. billing-events); target is set only for saga-commands.
func Enqueue(ctx context.Context, tx *sql.Tx, topic, target string, env Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, insertOutboxQuery,
		env.MessageID,
		topic,
		sql.NullString{String: target, Valid: target != ""},
		env.Type,
		string(body),
	)
	return err
}
