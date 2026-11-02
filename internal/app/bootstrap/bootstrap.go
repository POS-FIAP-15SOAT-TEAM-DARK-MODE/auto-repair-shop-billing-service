package bootstrap

import (
	"context"
	"database/sql"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	budgetRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	paymentRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/messaging"
	"go.uber.org/zap"
)

// Servers returns the HTTP API and, when configured, the outbox relay and SQS consumer.
func Servers() []app.Server {
	database := app.DBConnect()

	mode, err := payment.ParseMode(env.GetString("PAYMENT_MODE", "manual"))
	if err != nil {
		logger.Global().Fatal("invalid configuration", zap.Error(err))
	}

	idGen := id.NewIDGenerator()
	service := billing.NewService(
		database,
		budgetRepo.NewPostgres(),
		paymentRepo.NewPostgres(),
		payment.NewInternalProvider(mode),
		idGen.NewUUID,
	)

	controller := billing.NewController(service)
	var gatewayWorkers []app.Server
	if mode == payment.ModeMercadoPago {
		controller, gatewayWorkers, err = gatewayMode(service)
		if err != nil {
			logger.Global().Fatal("invalid configuration", zap.Error(err))
		}
	}

	router := routing.SetupRouter(
		&container.HandlersWrapper{Billing: controller},
		middlewaresContainer(),
	)

	servers := []app.Server{app.NewHTTPServer("http-api", ":"+env.GetString("PORT", "8083"), router)}
	servers = append(servers, gatewayWorkers...)
	return append(servers, messagingWorkers(database, service)...)
}

// messagingWorkers starts the relay and the consumer. They are disabled when the queue or topic is not configured.
func messagingWorkers(database *sql.DB, service *billing.Service) []app.Server {
	queueURL := env.GetString("BILLING_QUEUE_URL", "")
	eventsARN := env.GetString("BILLING_EVENTS_TOPIC_ARN", "")
	if queueURL == "" || eventsARN == "" {
		logger.Global().Warn("messaging disabled: BILLING_QUEUE_URL or BILLING_EVENTS_TOPIC_ARN is empty")
		return nil
	}

	ctx := context.Background()
	snsClient, err := messaging.NewSNSClient(ctx)
	if err != nil {
		logger.Global().Fatal("failed to create SNS client", zap.Error(err))
	}
	sqsClient, err := messaging.NewSQSClient(ctx)
	if err != nil {
		logger.Global().Fatal("failed to create SQS client", zap.Error(err))
	}

	relay := messaging.NewRelay(database, messaging.NewSNSPublisher(snsClient), map[string]string{"billing-events": eventsARN})
	consumer := messaging.NewConsumer(database, messaging.NewSQSQueue(sqsClient), queueURL)
	service.RegisterHandlers(consumer)

	return []app.Server{
		newWorker("outbox-relay", relay.Run),
		newWorker("sqs-consumer", consumer.Run),
	}
}

func middlewaresContainer() *container.Middlewares {
	return &container.Middlewares{
		"Logger":       middleware.Logger(),
		"Recovery":     middleware.Recovery(),
		"Metrics":      middleware.Metrics(),
		"ErrorHandler": middleware.ErrorHandler(),
	}
}
