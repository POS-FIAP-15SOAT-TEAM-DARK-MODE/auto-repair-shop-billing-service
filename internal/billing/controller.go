package billing

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const requestIDHeader = "X-Request-Id"

// Controller exposes the billing REST API. Route params are read from the context
// (gin params are copied there by routing.GinHandler).
type Controller struct {
	svc      *Service
	verifier SignatureVerifier
}

func NewController(svc *Service) *Controller {
	return &Controller{svc: svc}
}

// SignatureVerifier checks the x-signature header of a payment gateway webhook.
type SignatureVerifier interface {
	Verify(xSignature, xRequestID, dataID string) error
}

// WithWebhook enables the public webhook endpoint (mercadopago mode).
func (c *Controller) WithWebhook(v SignatureVerifier) *Controller {
	c.verifier = v
	return c
}

// WebhookEnabled reports whether the webhook route should be registered.
func (c *Controller) WebhookEnabled() bool { return c.verifier != nil && c.svc.gateway != nil }

type itemResponse struct {
	Kind      string `json:"kind"`
	RefID     string `json:"ref_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice string `json:"unit_price"`
}

type budgetResponse struct {
	ID             string         `json:"id"`
	ServiceOrderID string         `json:"service_order_id"`
	Status         string         `json:"status"`
	TotalAmount    string         `json:"total_amount"`
	Items          []itemResponse `json:"items"`
	CreatedAt      time.Time      `json:"created_at"`
}

type paymentResponse struct {
	ID             string    `json:"id"`
	BudgetID       string    `json:"budget_id"`
	ServiceOrderID string    `json:"service_order_id"`
	Amount         string    `json:"amount"`
	Status         string    `json:"status"`
	Provider       string    `json:"provider"`
	CreatedAt      time.Time `json:"created_at"`
	// Only for payments charged through the gateway: the link the attendant gives to the customer.
	CheckoutURL string     `json:"checkout_url,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// GetBudget: attendant, mechanic and admin see any budget; a customer sees only its own.
// Another customer's budget is reported as not found.
func (c *Controller) GetBudget(ctx context.Context, req *http.Request) (budgetResponse, error) {
	claims, err := auth.GetClaimsFromHeader(req)
	if err != nil {
		return budgetResponse{}, err
	}

	b, err := c.svc.GetBudget(ctx, paramFrom(ctx, "serviceOrderId"))
	if err != nil {
		return budgetResponse{}, err
	}
	if !auth.HasRequiredRoles(claims.Roles, auth.AttendantAndMechanicRoles) && claims.UserId != b.CustomerUserID {
		return budgetResponse{}, budget.ErrNotFound
	}
	return toBudgetResponse(b), nil
}

func (c *Controller) GetPayment(ctx context.Context, _ *http.Request) (paymentResponse, error) {
	p, err := c.svc.GetLatestPayment(ctx, paramFrom(ctx, "serviceOrderId"))
	if err != nil {
		return paymentResponse{}, err
	}
	return toPaymentResponse(p), nil
}

func (c *Controller) Confirm(ctx context.Context, req *http.Request) error {
	return c.svc.Confirm(ctx, paramFrom(ctx, "id"), requestID(req))
}

func (c *Controller) Reject(ctx context.Context, req *http.Request) error {
	return c.svc.Reject(ctx, paramFrom(ctx, "id"), requestID(req))
}

func paramFrom(ctx context.Context, key string) string {
	v, _ := ctx.Value(key).(string)
	return v
}

func requestID(req *http.Request) string {
	if id := req.Header.Get(requestIDHeader); id != "" {
		return id
	}
	return uuid.NewString()
}

func toBudgetResponse(b *budget.Budget) budgetResponse {
	items := make([]itemResponse, 0, len(b.Items))
	for _, it := range b.Items {
		items = append(items, itemResponse{
			Kind:      string(it.Kind),
			RefID:     it.RefID,
			Name:      it.Name,
			Quantity:  it.Quantity,
			UnitPrice: formatMoney(it.UnitPrice),
		})
	}
	return budgetResponse{
		ID:             b.ID,
		ServiceOrderID: b.ServiceOrderID,
		Status:         string(b.Status),
		TotalAmount:    formatMoney(b.TotalAmount),
		Items:          items,
		CreatedAt:      b.CreatedAt,
	}
}

func toPaymentResponse(p *payment.Payment) paymentResponse {
	return paymentResponse{
		ID:             p.ID,
		BudgetID:       p.BudgetID,
		ServiceOrderID: p.ServiceOrderID,
		Amount:         formatMoney(p.Amount),
		Status:         string(p.Status),
		Provider:       p.Provider,
		CreatedAt:      p.CreatedAt,
		CheckoutURL:    p.CheckoutURL,
		ExpiresAt:      p.ExpiresAt,
	}
}

const webhookMaxBody = 64 << 10

type webhookNotification struct {
	Type string `json:"type"`
	Data struct {
		ID json.RawMessage `json:"id"`
	} `json:"data"`
}

type webhookResponse struct {
	Result string `json:"result"`
}

// Webhook receives a gateway notification. It is public: the only authentication is the signature,
// which is checked before anything else is done. The body is never trusted for the payment data,
// only for the id; the payment is read from the gateway. A non-2xx answer makes the gateway retry.
func (c *Controller) Webhook(ctx context.Context, req *http.Request) (webhookResponse, error) {
	body, err := io.ReadAll(io.LimitReader(req.Body, webhookMaxBody+1))
	if err != nil {
		return webhookResponse{}, payment.ErrInvalidNotification
	}
	if len(body) > webhookMaxBody {
		// A real notification is a few hundred bytes. Cutting a bigger body short would hide it: refuse it.
		logger.Of(ctx).Warn("payment webhook rejected: body too large", zap.String("event", "webhook.rejected"), zap.String("reason", "body_too_large"), zap.Int("limit_bytes", webhookMaxBody))
		return webhookResponse{}, payment.ErrInvalidNotification
	}
	var n webhookNotification
	_ = json.Unmarshal(body, &n) // a malformed body is caught below by the signature or the id check

	q := req.URL.Query()
	dataID := q.Get("data.id") // the signed value is the one in the query string
	if dataID == "" {
		dataID = strings.Trim(string(n.Data.ID), `"`)
	}
	typ := q.Get("type")
	if typ == "" {
		typ = n.Type
	}

	if err := c.verifier.Verify(req.Header.Get("x-signature"), req.Header.Get("x-request-id"), dataID); err != nil {
		logger.Of(ctx).Warn("payment webhook rejected: invalid signature", zap.String("request_id", req.Header.Get("x-request-id")))
		return webhookResponse{}, payment.ErrInvalidSignature
	}
	if typ != "payment" {
		return webhookResponse{Result: "ignored"}, nil
	}
	if _, err := strconv.ParseUint(dataID, 10, 64); err != nil {
		return webhookResponse{}, payment.ErrInvalidNotification
	}

	result, err := c.svc.HandleGatewayNotification(ctx, dataID)
	if err != nil {
		return webhookResponse{}, err
	}
	return webhookResponse{Result: string(result)}, nil
}
