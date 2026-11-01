package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleCount(t *testing.T, method, route, status string) uint64 {
	t.Helper()

	hist, ok := metrics.HTTPRequestDuration.WithLabelValues(method, route, status).(prometheus.Histogram)
	require.True(t, ok)

	var m dto.Metric
	require.NoError(t, hist.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestMetricsMiddleware_ObservesMatchedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Metrics())
	router.GET("/metrics-test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	before := sampleCount(t, http.MethodGet, "/metrics-test", "200")

	req := httptest.NewRequest(http.MethodGet, "/metrics-test", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, before+1, sampleCount(t, http.MethodGet, "/metrics-test", "200"))
}

func TestMetricsMiddleware_UnmatchedRouteUsesFallbackLabel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Metrics())

	before := sampleCount(t, http.MethodGet, "unmatched", "404")

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, before+1, sampleCount(t, http.MethodGet, "unmatched", "404"))
}
