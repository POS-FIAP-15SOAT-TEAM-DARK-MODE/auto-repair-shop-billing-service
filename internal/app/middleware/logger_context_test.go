package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func captureRequestLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	restore := logger.ReplaceGlobal(zap.New(core))
	t.Cleanup(restore)
	return logs
}

func ctxField(e observer.LoggedEntry, key string) (string, bool) {
	for _, f := range e.Context {
		if f.Key == key {
			return f.String, true
		}
	}
	return "", false
}

func TestLoggerMiddleware_LogsCarryRequestIDAndEvents(t *testing.T) {
	logs := captureRequestLogs(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Logger())
	router.GET("/v1/budgets/:id", func(c *gin.Context) {
		logger.Of(c.Request.Context()).Info("budget read")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/budgets/abc", nil)
	req.Header.Set("X-Request-Id", "upstream-42")
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.secret.signature")
	router.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 3, logs.Len())
	events := []string{}
	for _, e := range logs.All() {
		rid, ok := ctxField(e, "request_id")
		require.True(t, ok)
		assert.Equal(t, "upstream-42", rid)
		ev, _ := ctxField(e, "event")
		events = append(events, ev)
		for _, f := range e.Context {
			assert.NotContains(t, f.String, "eyJhbGci", "a JWT must never reach the logs")
		}
	}
	assert.Equal(t, []string{"request.started", "", "request.completed"}, events)
}

func TestLoggerMiddleware_ErrorLinesAreRequestFailed(t *testing.T) {
	logs := captureRequestLogs(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Logger())
	router.GET("/fail", func(c *gin.Context) {
		_ = c.Error(errors.New("something broke"))
		c.Status(http.StatusInternalServerError)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fail", nil))

	var failed int
	for _, e := range logs.All() {
		if ev, _ := ctxField(e, "event"); ev == "request.failed" {
			failed++
			assert.True(t, strings.Contains(e.Message, "something broke"))
		}
	}
	assert.Equal(t, 1, failed)
}
