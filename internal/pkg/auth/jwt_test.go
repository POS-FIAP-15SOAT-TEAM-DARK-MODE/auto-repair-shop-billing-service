package auth

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestHasRequiredRoles(t *testing.T) {
	tests := []struct {
		name       string
		userRoles  []Role
		routeRoles []Role
		want       bool
	}{
		{
			name:       "user has one required role",
			userRoles:  []Role{MECHANIC},
			routeRoles: []Role{MECHANIC, ADMIN},
			want:       true,
		},
		{
			name:       "user has no required role",
			userRoles:  []Role{CUSTOMER},
			routeRoles: []Role{MECHANIC, ADMIN},
			want:       false,
		},
		{
			name:       "user has multiple roles, one matches",
			userRoles:  []Role{CUSTOMER, ATTENDANT},
			routeRoles: []Role{ATTENDANT, ADMIN},
			want:       true,
		},
		{
			name:       "user has all roles needed",
			userRoles:  []Role{ADMIN, ATTENDANT},
			routeRoles: []Role{ATTENDANT, ADMIN},
			want:       true,
		},
		{
			name:       "empty user roles",
			userRoles:  []Role{},
			routeRoles: []Role{CUSTOMER},
			want:       false,
		},
		{
			name:       "empty route roles",
			userRoles:  []Role{ADMIN},
			routeRoles: []Role{},
			want:       false,
		},
		{
			name:       "both user and route roles empty",
			userRoles:  []Role{},
			routeRoles: []Role{},
			want:       false,
		},
		{
			name:       "user and route both ADMIN",
			userRoles:  []Role{ADMIN},
			routeRoles: []Role{ADMIN},
			want:       true,
		},
		{
			name:       "user has duplicate roles, matches",
			userRoles:  []Role{CUSTOMER, CUSTOMER},
			routeRoles: []Role{CUSTOMER},
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := HasRequiredRoles(test.userRoles, test.routeRoles)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestGenerateToken(t *testing.T) {
	cases := []struct {
		name      string
		roles     []Role
		expiresAt time.Time
	}{
		{"valid", []Role{ADMIN}, time.Now().Add(time.Hour)},
		{"expired", []Role{CUSTOMER}, time.Now().Add(-time.Hour)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := GenerateToken("user-id", tc.roles, tc.expiresAt)
			if err != nil {
				t.Fatalf("GenerateToken returned error: %v", err)
			}
			if token == "" {
				t.Fatalf("expected non-empty token")
			}
		})
	}
}

func TestGetClaimsFromHeader(t *testing.T) {
	validTok, err := GenerateToken("user-99", []Role{ADMIN}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name       string
		header     string
		wantErr    bool
		wantUserID string
	}{
		{
			name:       "valid bearer token",
			header:     "Bearer " + validTok,
			wantErr:    false,
			wantUserID: "user-99",
		},
		{
			name:    "empty header",
			header:  "",
			wantErr: true,
		},
		{
			name:    "no bearer prefix",
			header:  validTok,
			wantErr: true,
		},
		{
			name:    "bearer with expired token",
			header:  "Bearer invalid.token.here",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			claims, err := GetClaimsFromHeader(req)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantUserID, claims.UserId)
		})
	}
}

func TestParseToken(t *testing.T) {
	tests := []struct {
		name        string
		tokenMaker  func() (string, error)
		expectError bool
		expired     bool
	}{
		{
			name: "valid token",
			tokenMaker: func() (string, error) {
				return GenerateToken("user-1", []Role{ADMIN}, time.Now().Add(time.Hour))
			},
			expectError: false,
		},
		{
			name: "expired token",
			tokenMaker: func() (string, error) {
				return GenerateToken("user-2", []Role{CUSTOMER}, time.Now().Add(-time.Hour))
			},
			expectError: true,
			expired:     true,
		},
		{
			name: "invalid token string",
			tokenMaker: func() (string, error) {
				return "not-a-valid.token", nil
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := tt.tokenMaker()
			if err != nil {
				t.Fatalf("token maker error: %v", err)
			}
			claims, err := GetClaims(token)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error parsing token, got nil")
				}
				if tt.expired && !errors.Is(err, jwt.ErrTokenExpired) {
					t.Fatalf("expected expired error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if claims.UserId == "" {
				t.Fatalf("expected user id in claims")
			}
			assert.Contains(t, claims.Roles, ADMIN)
		})
	}
}
