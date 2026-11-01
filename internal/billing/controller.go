package billing

import (
	"context"
	"net/http"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-Id"

// Controller exposes the billing REST API. Route params are read from the context
// (gin params are copied there by routing.GinHandler).
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller {
	return &Controller{svc: svc}
}

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
	}
}
