package main

import (
	"context"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/bootstrap"
	pkgApp "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
)

func main() {
	defer logger.Global().Sync()

	if err := auth.RequireSecret(); err != nil {
		logger.Global().Fatal("config.invalid: JWT_SECRET is required")
	}

	manager := pkgApp.NewLifecycleManager(5 * time.Second)
	manager.Add(bootstrap.Servers()...)

	if err := manager.Run(context.Background()); err != nil {
		logger.Global().Fatal("application failed")
	}
}
