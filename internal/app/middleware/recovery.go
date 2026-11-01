package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				logger.Of(c).Error(err.(error),
					zap.String("behavior", "[PANIC RECOVERED]"),
					zap.String("stack", string(stack)),
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "internal server error",
					"details": fmt.Sprintf("%v", err),
				})
			}
		}()

		c.Next()
	}
}

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		errs := c.Errors.Errors()

		status := c.Writer.Status()
		if status == http.StatusOK {
			status = http.StatusInternalServerError
		}

		c.JSON(status, gin.H{
			"error":   http.StatusText(status),
			"details": errs,
		})
	}
}
