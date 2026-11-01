package billing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture loads testdata/contracts/<name>.json and returns the envelope and its raw payload.
func fixture(t *testing.T, name string) ([]byte, json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", name+".json"))
	require.NoError(t, err)
	var env struct {
		Payload json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	return raw, env.Payload
}

// Consumed commands: the fixture must decode into the handler's payload type.
func TestContract_ConsumedCommands(t *testing.T) {
	_, payload := fixture(t, "GenerateBudget")
	var gen generateBudgetPayload
	require.NoError(t, json.Unmarshal(payload, &gen))
	works, supplies, err := toLines(gen)
	require.NoError(t, err)
	assert.Equal(t, "3b123072-5ad5-4df8-8d3b-3c2770f11b2a", gen.CustomerUserID)
	assert.Len(t, works, 1)
	assert.Len(t, supplies, 1)
	assert.Equal(t, 2, supplies[0].Quantity)

	_, payload = fixture(t, "RequestPayment")
	var req requestPaymentPayload
	require.NoError(t, json.Unmarshal(payload, &req))
	assert.Equal(t, "221.00", req.Amount)

	for _, name := range []string{"CancelBudget", "RefundPayment"} {
		_, p := fixture(t, name)
		assert.JSONEq(t, "{}", string(p), name)
	}
}

// Published events: the payload built by billing must equal the fixture.
func TestContract_PublishedEvents(t *testing.T) {
	cases := map[string]any{
		"BudgetGenerated": budgetGeneratedPayload{BudgetID: "a1d9c2e4-0b7f-4c1e-9f7e-2b6d5c8e4f10", TotalAmount: formatMoney(decimal.RequireFromString("221"))},
		"BudgetCancelled": budgetCancelledPayload{BudgetID: "a1d9c2e4-0b7f-4c1e-9f7e-2b6d5c8e4f10"},
		"PaymentApproved": paymentApprovedPayload{PaymentID: "c3e8f1a2-5d6b-4e9a-8b7c-1f2e3d4c5b6a", Amount: formatMoney(decimal.RequireFromString("221"))},
		"PaymentFailed":   paymentFailedPayload{PaymentID: "c3e8f1a2-5d6b-4e9a-8b7c-1f2e3d4c5b6a", Amount: "221.00", Reason: reasonPaymentRejected},
		"PaymentRefunded": paymentRefundedPayload{PaymentID: "c3e8f1a2-5d6b-4e9a-8b7c-1f2e3d4c5b6a", Amount: "221.00"},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, want := fixture(t, name)
			got, err := json.Marshal(payload)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
		})
	}
}

func TestContract_EnvelopeOfEventsCarriesSagaFields(t *testing.T) {
	raw, _ := fixture(t, "PaymentApproved")
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	for _, k := range []string{"message_id", "saga_id", "type", "occurred_at", "service_order_id", "request_id", "payload"} {
		assert.Contains(t, m, k)
	}
}
