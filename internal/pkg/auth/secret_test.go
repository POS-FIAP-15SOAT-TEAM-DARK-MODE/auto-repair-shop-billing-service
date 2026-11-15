package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequireSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	assert.ErrorIs(t, RequireSecret(), ErrJWTSecretMissing)

	t.Setenv("JWT_SECRET", "a-real-secret")
	assert.NoError(t, RequireSecret())
}
