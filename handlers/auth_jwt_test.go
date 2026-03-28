package handlers

import (
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// TestGenerateJWT_IncludesExpAndJti documents that access tokens issued at login/signup
// carry exp and jti as required by middleware.Auth (jti is RegisteredClaims.ID).
func TestGenerateJWT_IncludesExpAndJti(t *testing.T) {
	const secret = "test-secret-key-minimum-length-for-security-1234"
	os.Setenv("JWT_SECRET", secret)
	os.Setenv("JWT_EXPIRY_MINUTES", "15")
	t.Cleanup(func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("JWT_EXPIRY_MINUTES")
	})

	tokenStr, _, err := generateJWT(42)
	require.NoError(t, err)
	require.NotEmpty(t, tokenStr)

	parsed, err := jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)

	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	require.True(t, ok, "claims should decode as RegisteredClaims")
	require.Equal(t, "42", claims.Subject)
	require.NotEmpty(t, claims.ID, "jti (RegisteredClaims.ID) must be set for protected routes")
	require.NotNil(t, claims.ExpiresAt)
	require.False(t, claims.ExpiresAt.Time.Before(time.Now().Add(-time.Minute)), "exp should be in the future")
}
