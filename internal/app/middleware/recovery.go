package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/logger"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				logger.Of(c).Error(fmt.Errorf("panic: %v", rec),
					zap.String("event", "request.panic"),
					zap.String("behavior", "[PANIC RECOVERED]"),
					zap.String("stack", string(stack)),
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":  http.StatusInternalServerError,
					"error": "internal server error",
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

		status := c.Writer.Status()
		if status == http.StatusOK {
			status = http.StatusInternalServerError
		}

		// The errors are already logged by the Logger middleware; the body stays generic.
		c.JSON(status, gin.H{
			"code":  status,
			"error": strings.ToLower(http.StatusText(status)),
		})
	}
}
