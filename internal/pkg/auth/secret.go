package auth

import (
	"errors"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
)

// ErrJWTSecretMissing is returned when the service starts without JWT_SECRET.
var ErrJWTSecretMissing = errors.New("JWT_SECRET is required")

// RequireSecret refuses a start without JWT_SECRET: the built-in default exists only for tests and
// would let anyone who reads the source sign valid tokens.
func RequireSecret() error {
	if env.GetString("JWT_SECRET", "") == "" {
		return ErrJWTSecretMissing
	}
	return nil
}
