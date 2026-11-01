package auth

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
	"github.com/golang-jwt/jwt/v5"
)

type UserClaims struct {
	UserId string `json:"user_id"`
	Roles  []Role `json:"roles"`
	jwt.RegisteredClaims
}

var ErrInvalidUserCredentials = errors.New("invalid user credentials")

var secretKey []byte

const (
	secretDefaultValue  = "your_jwt_secret"
	authorizationHeader = "Authorization"
	bearerPrefix        = "Bearer "
)

func init() {
	secretKey = []byte(env.GetString("JWT_SECRET", secretDefaultValue))
}

func GenerateToken(userId string, roles []Role, expiresAt time.Time) (string, error) {
	claims := UserClaims{
		UserId: userId,
		Roles:  roles,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secretKey)
}

func GetClaimsFromHeader(req *http.Request) (*UserClaims, error) {
	token := req.Header.Get(authorizationHeader)
	if token == "" || !strings.HasPrefix(token, bearerPrefix) {
		return nil, ErrInvalidUserCredentials
	}

	token = strings.TrimPrefix(token, bearerPrefix)
	return GetClaims(token)
}

func GetClaims(token string) (*UserClaims, error) {
	claims := &UserClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		return secretKey, nil
	})
	return claims, err
}

func HasRequiredRoles(userRoles []Role, rolesNeeded []Role) bool {
	for _, role := range userRoles {
		if slices.Contains(rolesNeeded, role) {
			return true
		}
	}
	return false
}
