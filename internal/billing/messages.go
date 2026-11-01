package billing

// Payloads of the messages consumed and published by billing (contracts/messaging.md).
// Money is always a decimal string with 2 places, e.g. "221.00".

type generateBudgetPayload struct {
	CustomerUserID string          `json:"customer_user_id"`
	Works          []workPayload   `json:"works"`
	Supplies       []supplyPayload `json:"supplies"`
}

type workPayload struct {
	WorkID    string `json:"work_id"`
	Name      string `json:"name"`
	UnitPrice string `json:"unit_price"`
}

type supplyPayload struct {
	SupplyID  string `json:"supply_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice string `json:"unit_price"`
}

type requestPaymentPayload struct {
	Amount string `json:"amount"`
}

type budgetGeneratedPayload struct {
	BudgetID    string `json:"budget_id"`
	TotalAmount string `json:"total_amount"`
}

type budgetCancelledPayload struct {
	BudgetID string `json:"budget_id"`
}

type paymentApprovedPayload struct {
	PaymentID string `json:"payment_id"`
	Amount    string `json:"amount"`
}

type paymentFailedPayload struct {
	PaymentID string `json:"payment_id"`
	Amount    string `json:"amount"`
	Reason    string `json:"reason"`
}

type paymentRefundedPayload struct {
	PaymentID string `json:"payment_id"`
	Amount    string `json:"amount"`
}

const reasonPaymentRejected = "PAYMENT_REJECTED"

const (
	topicBillingEvents = "billing-events"

	cmdGenerateBudget = "GenerateBudget"
	cmdCancelBudget   = "CancelBudget"
	cmdRequestPayment = "RequestPayment"
	cmdRefundPayment  = "RefundPayment"

	evBudgetGenerated = "BudgetGenerated"
	evBudgetCancelled = "BudgetCancelled"
	evPaymentApproved = "PaymentApproved"
	evPaymentFailed   = "PaymentFailed"
	evPaymentRefunded = "PaymentRefunded"
)
