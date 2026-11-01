package messaging

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEnvelope(t *testing.T) {
	env, err := NewEnvelope("BudgetCancelled", "saga-1", "SO-1", "req-1", map[string]string{"budget_id": "b"})
	require.NoError(t, err)

	assert.Len(t, env.MessageID, 36)
	assert.Equal(t, "BudgetCancelled", env.Type)
	assert.Equal(t, "saga-1", env.SagaID)
	assert.Equal(t, "SO-1", env.ServiceOrderID)
	assert.Equal(t, "req-1", env.RequestID)
	assert.Equal(t, time.UTC, env.OccurredAt.Location())
	assert.JSONEq(t, `{"budget_id":"b"}`, string(env.Payload))
}

func TestNewEnvelope_PayloadNotMarshalable(t *testing.T) {
	_, err := NewEnvelope("X", "", "", "", make(chan int))
	assert.Error(t, err)
}

func TestDecode(t *testing.T) {
	valid := `{"message_id":"b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11","type":"CancelBudget","payload":{}}`

	env, err := Decode([]byte(valid))
	require.NoError(t, err)
	assert.Equal(t, "CancelBudget", env.Type)

	cases := map[string]string{
		"invalid json":    `{"message_id":`,
		"invalid uuid":    `{"message_id":"nope","type":"CancelBudget"}`,
		"missing type":    `{"message_id":"b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11"}`,
		"wrong field typ": `{"message_id":"b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11","type":"X","occurred_at":5}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(body))
			assert.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidEnvelope)
		})
	}
}

func TestContractFixtureRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/contracts/GenerateBudget.json")
	require.NoError(t, err)

	env, err := Decode(raw)
	require.NoError(t, err)
	assert.Equal(t, "GenerateBudget", env.Type)
	assert.Equal(t, "2026-11-01T12:00:00Z", env.OccurredAt.Format(time.RFC3339))

	out, err := json.Marshal(env)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(out))
}
