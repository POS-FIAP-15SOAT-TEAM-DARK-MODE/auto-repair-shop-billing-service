package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/mercadopago"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/db/postgres"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
)

// gatewayWorkerLockKey identifies the advisory lock the replicas share to run the gateway worker one at a time.
const gatewayWorkerLockKey int64 = 0x42494c4c47574b52 // "BILLGWKR"

// gatewayMode wires the mercadopago mode from the environment: the Mercado Pago gateway, the webhook
// verifier and the gateway worker. It fails when the access token is missing, so the service never
// starts in a mode where it could not charge.
//
//	MERCADOPAGO_ACCESS_TOKEN       required
//	MERCADOPAGO_WEBHOOK_SECRET     signs the webhook; empty rejects every webhook
//	MERCADOPAGO_NOTIFICATION_URL   public HTTPS URL of the webhook; empty means "do not send"
//	MERCADOPAGO_CHECKOUT_TTL       lifetime of a checkout (default 12h)
//	MERCADOPAGO_WEBHOOK_TOLERANCE  accepted distance of the signature timestamp (default 5m)
//	GATEWAY_WORKER_INTERVAL        how often the worker runs (default 5s)
func gatewayMode(service *billing.Service, database *sql.DB) (*billing.Controller, []app.Server, error) {
	token := env.GetString("MERCADOPAGO_ACCESS_TOKEN", "")
	if token == "" {
		return nil, nil, errors.New("MERCADOPAGO_ACCESS_TOKEN is required when PAYMENT_MODE=mercadopago")
	}
	gateway, err := mercadopago.New(token)
	if err != nil {
		return nil, nil, err
	}

	secret := env.GetString("MERCADOPAGO_WEBHOOK_SECRET", "")
	if secret == "" {
		logger.Global().Warn("MERCADOPAGO_WEBHOOK_SECRET is empty: every webhook will be rejected; payments settle through the reconciliation worker only")
	}
	service.WithGateway(billing.GatewayConfig{
		Gateway:         mercadopago.Instrument(gateway),
		NotificationURL: env.GetString("MERCADOPAGO_NOTIFICATION_URL", ""),
		CheckoutTTL:     env.GetTimeDuration("MERCADOPAGO_CHECKOUT_TTL", 12*time.Hour),
		WorkLock:        postgres.NewAdvisoryLock(database, gatewayWorkerLockKey),
	})
	controller := billing.NewController(service).WithWebhook(mercadopago.Verifier{
		Secret:    secret,
		Tolerance: env.GetTimeDuration("MERCADOPAGO_WEBHOOK_TOLERANCE", 5*time.Minute),
	})

	interval := env.GetTimeDuration("GATEWAY_WORKER_INTERVAL", 5*time.Second)
	worker := newWorker("gateway-worker", func(ctx context.Context) error { return service.RunGatewayWorker(ctx, interval) })
	return controller, []app.Server{worker}, nil
}
