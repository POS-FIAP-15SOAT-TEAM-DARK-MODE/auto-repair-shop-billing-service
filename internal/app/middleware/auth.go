package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	authorizationHeader = "Authorization"
	bearerPrefix        = "Bearer "
)

func Auth(routeRoles ...auth.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(authorizationHeader)
		if token == "" || !strings.HasPrefix(token, bearerPrefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		token = strings.TrimPrefix(token, bearerPrefix)

		userClaims, err := auth.GetClaims(token)
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
				c.Abort()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		if !auth.HasRequiredRoles(userClaims.Roles, routeRoles) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
			c.Abort()
			return
		}
	}
}
