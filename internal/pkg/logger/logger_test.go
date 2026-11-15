package logger_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestGlobal(t *testing.T) {
	l := logger.Global()
	assert.NotNil(t, l)
}

func TestOf_NilContext(t *testing.T) {
	l := logger.Of(context.TODO())
	assert.Equal(t, logger.Global(), l)
}

func TestRequest(t *testing.T) {
	ctx := context.Background()
	ctx, reqId := logger.Request(ctx, "")

	assert.NotEmpty(t, reqId)
	l := logger.Of(ctx)
	assert.NotNil(t, l)
	assert.NotEqual(t, logger.Global(), l)
}

func TestRequest_ReusesIncomingID(t *testing.T) {
	ctx := context.Background()
	_, reqId := logger.Request(ctx, "upstream-request-id-123")

	assert.Equal(t, "upstream-request-id-123", reqId)
}

func TestLoggerMethods(t *testing.T) {
	l := logger.Global()

	l.Debug("debug message")
	l.Info("info message")
	l.Warn("warn message")
	l.Error(errors.New("error message"))

	l.Info("message with fields", zap.String("key", "value"))
}

func TestSync(t *testing.T) {
	l := logger.Global()
	l.Sync()
}

func TestOf_NoLoggerInContext(t *testing.T) {
	ctx := context.Background()
	l := logger.Of(ctx)
	assert.Equal(t, logger.Global(), l)
}

func TestGetLogLevel(t *testing.T) {
	assert.Equal(t, logger.Global(), logger.Of(context.Background()))
}

func TestGetLogLevel_Debug(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	ctx := context.Background()
	ctx, reqId := logger.Request(ctx, "")
	assert.NotEmpty(t, reqId)
	l := logger.Of(ctx)
	assert.NotNil(t, l)
}

func TestGetLogLevel_Panic(t *testing.T) {
	t.Setenv("LOG_LEVEL", "PANIC")
	ctx := context.Background()
	ctx, reqId := logger.Request(ctx, "")
	assert.NotEmpty(t, reqId)
	l := logger.Of(ctx)
	assert.NotNil(t, l)
}

func TestFatalProcess(t *testing.T) {
	if os.Getenv("TEST_FATAL") == "1" {
		logger.Global().Fatal("fatal message")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestFatalProcess")
	cmd.Env = append(os.Environ(), "TEST_FATAL=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatal("expected process to exit with failure")
}
