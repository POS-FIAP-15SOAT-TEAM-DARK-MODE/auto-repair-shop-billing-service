package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
)

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.Contains(path, "swagger") {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		ctx, reqId := logger.Request(ctx, c.GetHeader("X-Request-Id"))
		c.Request = c.Request.WithContext(ctx)

		c.Writer.Header().Set("X-Request-ID", reqId)

		start := time.Now()
		query := c.Request.URL.RawQuery
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
		}

		logger.Of(ctx).Info("starting request", append(fields, zap.String("event", "request.started"))...)

		c.Next()

		fields = append(fields,
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)))

		if len(c.Errors) > 0 {
			for _, e := range c.Errors.Errors() {
				logger.Of(ctx).Error(errors.New(e), append(fields, zap.String("event", "request.failed"))...)
			}
			return
		}

		logger.Of(ctx).Info("ending request", append(fields, zap.String("event", "request.completed"))...)
	}
}
