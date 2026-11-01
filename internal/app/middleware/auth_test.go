package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddleware_TableDriven(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name             string
		middlewareRoles  []auth.Role
		tokenRoles       []auth.Role
		tokenExpiry      time.Time
		customHeader     string
		expectedHTTPCode int
	}{
		{
			name:             "missing header",
			middlewareRoles:  []auth.Role{auth.ADMIN},
			tokenRoles:       nil,
			expectedHTTPCode: http.StatusUnauthorized,
		},
		{
			name:             "malformed header",
			middlewareRoles:  []auth.Role{auth.ADMIN},
			customHeader:     "Token abc.def",
			expectedHTTPCode: http.StatusUnauthorized,
		},
		{
			name:             "invalid token",
			middlewareRoles:  []auth.Role{auth.ADMIN},
			customHeader:     "Bearer not-a-valid.token",
			expectedHTTPCode: http.StatusUnauthorized,
		},
		{
			name:             "expired token",
			middlewareRoles:  []auth.Role{auth.CUSTOMER},
			tokenRoles:       []auth.Role{auth.CUSTOMER},
			tokenExpiry:      time.Now().Add(-time.Hour),
			expectedHTTPCode: http.StatusForbidden,
		},
		{
			name:             "roles mismatch",
			middlewareRoles:  []auth.Role{auth.ADMIN},
			tokenRoles:       []auth.Role{auth.CUSTOMER},
			tokenExpiry:      time.Now().Add(time.Hour),
			expectedHTTPCode: http.StatusForbidden,
		},
		{
			name:             "roles match",
			middlewareRoles:  []auth.Role{auth.ATTENDANT},
			tokenRoles:       []auth.Role{auth.ATTENDANT, auth.ADMIN},
			tokenExpiry:      time.Now().Add(time.Hour),
			expectedHTTPCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			// protected route that returns 200 when reached
			router.GET("/protected", Auth(tt.middlewareRoles...), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			// set header according to test case
			if tt.customHeader != "" {
				req.Header.Set(authorizationHeader, tt.customHeader)
			} else if tt.tokenRoles != nil {
				tok, err := auth.GenerateToken("test-user", tt.tokenRoles, tt.tokenExpiry)
				if err != nil {
					t.Fatalf("failed to generate token: %v", err)
				}
				req.Header.Set(authorizationHeader, bearerPrefix+tok)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedHTTPCode, rec.Code)
		})
	}
}
