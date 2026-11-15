package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestServiceName(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("SERVICE_NAME", "")
		assert.Equal(t, "billing-service", serviceName())
	})
	t.Run("from env", func(t *testing.T) {
		t.Setenv("SERVICE_NAME", "billing-custom")
		assert.Equal(t, "billing-custom", serviceName())
	})
}

func TestProductionConfig_InitialFieldsCarryService(t *testing.T) {
	t.Setenv("SERVICE_NAME", "billing-custom")
	cfg := productionConfig(zap.NewAtomicLevelAt(zap.InfoLevel))
	assert.Equal(t, "billing-custom", cfg.InitialFields["service"])
	assert.Equal(t, "timestamp", cfg.EncoderConfig.TimeKey)
	assert.Equal(t, "message", cfg.EncoderConfig.MessageKey)
}
