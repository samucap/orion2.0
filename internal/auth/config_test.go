package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_RejectsMissingSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	_, err := LoadConfig()
	require.Error(t, err)
}

func TestLoadConfig_RejectsShortSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("a", MinJWTSecretBytes-1))
	_, err := LoadConfig()
	require.Error(t, err)
}

func TestLoadConfig_RejectsInvalidExpiry(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	t.Setenv("JWT_EXPIRY_MINUTES", "abc")
	_, err := LoadConfig()
	require.Error(t, err)
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	t.Setenv("JWT_EXPIRY_MINUTES", "")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, DefaultAccessTTL, cfg.AccessTTL)
	assert.Equal(t, DefaultRefreshTTL, cfg.RefreshTTL)
}

func TestIssueAccessToken_IncludesSubExpJti(t *testing.T) {
	cfg, err := NewConfig(testSecret, 15*time.Minute)
	require.NoError(t, err)

	tokenStr, expiresAt, err := IssueAccessToken(cfg, 42)
	require.NoError(t, err)

	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	assert.Equal(t, "42", claims.Subject)
	assert.NotEmpty(t, claims.ID)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), expiresAt, 5*time.Second)
}
