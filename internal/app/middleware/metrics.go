package middleware

import (
	"strconv"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/metrics"
	"github.com/gin-gonic/gin"
)

// Metrics records request latency per method/route/status. The route label
// uses c.FullPath() (the matched route template, e.g. "/v1/service-order/:id")
// rather than the raw path, so per-request IDs never blow up cardinality.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		metrics.HTTPRequestDuration.WithLabelValues(
			c.Request.Method,
			route,
			strconv.Itoa(c.Writer.Status()),
		).Observe(time.Since(start).Seconds())
	}
}
