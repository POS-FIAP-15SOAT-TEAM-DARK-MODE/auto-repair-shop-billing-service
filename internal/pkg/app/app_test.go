package app_test

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockServer is a mock of the server.Server interface
type MockServer struct {
	mock.Mock
}

func (m *MockServer) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockServer) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockServer) Name() string {
	args := m.Called()
	return args.String(0)
}

func TestLifecycleManager(t *testing.T) {
	t.Run("NewLifecycleManager", func(t *testing.T) {
		timeout := 5 * time.Second
		m := app.NewLifecycleManager(timeout)
		assert.NotNil(t, m)
	})

	t.Run("Add and Run successful", func(t *testing.T) {
		timeout := 100 * time.Millisecond
		m := app.NewLifecycleManager(timeout)

		srv1 := new(MockServer)
		srv1.On("Start", mock.Anything).Return(nil)
		srv1.On("Stop", mock.Anything).Return(nil)

		m.Add(srv1)

		ctx, cancel := context.WithCancel(context.Background())

		// Run in a goroutine because it blocks until signal or context cancel
		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Run(ctx)
		}()

		// Give it a moment to start
		time.Sleep(50 * time.Millisecond)
		cancel()

		err := <-errCh
		assert.NoError(t, err)
		srv1.AssertExpectations(t)
	})

	t.Run("Run via OS signal", func(t *testing.T) {
		timeout := 100 * time.Millisecond
		m := app.NewLifecycleManager(timeout)

		srv1 := new(MockServer)
		srv1.On("Start", mock.Anything).Return(nil)
		srv1.On("Stop", mock.Anything).Return(nil)

		m.Add(srv1)

		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Run(context.Background())
		}()

		time.Sleep(50 * time.Millisecond)
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGTERM)

		err := <-errCh
		assert.NoError(t, err)
		srv1.AssertExpectations(t)
	})

	t.Run("Run with server start error", func(t *testing.T) {
		timeout := 100 * time.Millisecond
		m := app.NewLifecycleManager(timeout)

		srv1 := new(MockServer)
		srv1.On("Name").Return("server-1")
		srv1.On("Start", mock.Anything).Return(errors.New("start failed"))
		srv1.On("Stop", mock.Anything).Return(nil)

		m.Add(srv1)

		err := m.Run(context.Background())
		assert.Error(t, err)
		assert.Equal(t, "start failed", err.Error())
	})

	t.Run("Run with server stop error", func(t *testing.T) {
		timeout := 100 * time.Millisecond
		m := app.NewLifecycleManager(timeout)

		srv1 := new(MockServer)
		srv1.On("Name").Return("server-1")
		srv1.On("Start", mock.Anything).Return(nil)
		srv1.On("Stop", mock.Anything).Return(errors.New("stop failed"))

		m.Add(srv1)

		ctx, cancel := context.WithCancel(context.Background())

		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Run(ctx)
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()

		err := <-errCh
		assert.NoError(t, err)
		srv1.AssertExpectations(t)
	})
}
