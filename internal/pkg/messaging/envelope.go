package messaging

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrInvalidEnvelope is returned when a message body is not a valid envelope.
var ErrInvalidEnvelope = errors.New("invalid message envelope")

// Envelope is the JSON body of every message (see contracts/messaging.md).
type Envelope struct {
	MessageID      string          `json:"message_id"`
	SagaID         string          `json:"saga_id"`
	Type           string          `json:"type"`
	OccurredAt     time.Time       `json:"occurred_at"`
	ServiceOrderID string          `json:"service_order_id"`
	RequestID      string          `json:"request_id"`
	Payload        json.RawMessage `json:"payload"`
}

// NewEnvelope builds an envelope with a new UUID v4 message_id and occurred_at in UTC.
func NewEnvelope(typ, sagaID, serviceOrderID, requestID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		MessageID:      uuid.NewString(),
		SagaID:         sagaID,
		Type:           typ,
		OccurredAt:     time.Now().UTC(),
		ServiceOrderID: serviceOrderID,
		RequestID:      requestID,
		Payload:        raw,
	}, nil
}

// Decode parses and validates a message body.
func Decode(body []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Envelope{}, errors.Join(ErrInvalidEnvelope, err)
	}
	if _, err := uuid.Parse(env.MessageID); err != nil {
		return Envelope{}, errors.Join(ErrInvalidEnvelope, err)
	}
	if env.Type == "" {
		return Envelope{}, ErrInvalidEnvelope
	}
	return env, nil
}

// logFields are the identifiers every log line about a message carries.
// Ids that the message does not have (e.g. saga_id on a plain event) are left out.
func (e Envelope) logFields() []zap.Field {
	fields := []zap.Field{zap.String("message_id", e.MessageID), zap.String("type", e.Type)}
	if e.RequestID != "" {
		fields = append(fields, zap.String("request_id", e.RequestID))
	}
	if e.SagaID != "" {
		fields = append(fields, zap.String("saga_id", e.SagaID))
	}
	if e.ServiceOrderID != "" {
		fields = append(fields, zap.String("service_order_id", e.ServiceOrderID))
	}
	return fields
}
