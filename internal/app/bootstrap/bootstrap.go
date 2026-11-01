package bootstrap

import (
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
)

func HTTPServer() app.Server {
	router := routing.SetupRouter(&container.HandlersWrapper{}, middlewaresContainer())

	port := env.GetString("PORT", "8083")
	return app.NewHTTPServer("http-api", ":"+port, router)
}

func middlewaresContainer() *container.Middlewares {
	return &container.Middlewares{
		"Logger":       middleware.Logger(),
		"Recovery":     middleware.Recovery(),
		"Metrics":      middleware.Metrics(),
		"ErrorHandler": middleware.ErrorHandler(),
	}
}
