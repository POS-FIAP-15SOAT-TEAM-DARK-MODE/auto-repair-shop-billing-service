package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type LifecycleManager struct {
	servers []app.Server
	timeout time.Duration
}

func NewLifecycleManager(timeout time.Duration) *LifecycleManager {
	return &LifecycleManager{
		servers: make([]app.Server, 0),
		timeout: timeout,
	}
}

func (m *LifecycleManager) Add(s ...app.Server) {
	m.servers = append(m.servers, s...)
}

func (m *LifecycleManager) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, s := range m.servers {
		srv := s
		g.Go(func() error {
			return srv.Start(ctx)
		})
	}

	// Wait for OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logger.Global().Info("received shutdown signal")
	case <-ctx.Done():
		logger.Global().Info("context cancelled")
	}

	// Shutdown process
	logger.Global().Info("shutting down all servers...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	for _, s := range m.servers {
		srv := s
		if err := srv.Stop(shutdownCtx); err != nil {
			logger.Global().Error(fmt.Errorf("server forced to shutdown: %w", err), zap.String("name", srv.Name()))
		}
	}

	return g.Wait()
}
