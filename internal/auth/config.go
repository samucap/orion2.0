package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// MinJWTSecretBytes is the minimum HS256 key length (RFC 7518 §3.2:
	// the key must be at least as long as the 256-bit hash output).
	MinJWTSecretBytes = 32

	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 7 * 24 * time.Hour
)

// Config is the single source of truth for token signing and lifetimes.
type Config struct {
	JWTSecret  []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// LoadConfig reads JWT_SECRET (required, >= 32 bytes) and
// JWT_EXPIRY_MINUTES (optional, default 15) from the environment.
func LoadConfig() (Config, error) {
	accessTTL := DefaultAccessTTL
	if v := os.Getenv("JWT_EXPIRY_MINUTES"); v != "" {
		m, err := strconv.Atoi(v)
		if err != nil || m <= 0 {
			return Config{}, fmt.Errorf("invalid JWT_EXPIRY_MINUTES %q: must be a positive integer", v)
		}
		accessTTL = time.Duration(m) * time.Minute
	}
	return NewConfig(os.Getenv("JWT_SECRET"), accessTTL)
}

// NewConfig validates the secret and builds a Config with the default
// refresh TTL. Tests use it to avoid depending on environment variables.
func NewConfig(secret string, accessTTL time.Duration) (Config, error) {
	if secret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	if len(secret) < MinJWTSecretBytes {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least %d bytes (got %d); generate one with: openssl rand -base64 48", MinJWTSecretBytes, len(secret))
	}
	if accessTTL <= 0 {
		accessTTL = DefaultAccessTTL
	}
	return Config{
		JWTSecret:  []byte(secret),
		AccessTTL:  accessTTL,
		RefreshTTL: DefaultRefreshTTL,
	}, nil
}

// IssueAccessToken mints a short-lived HS256 JWT (sub, iat, exp, jti).
// It is the only access-token issuer in the codebase.
func IssueAccessToken(cfg Config, userID int64) (string, time.Time, error) {
	if len(cfg.JWTSecret) == 0 {
		return "", time.Time{}, errors.New("auth config not initialized")
	}

	now := time.Now()
	expiresAt := now.Add(cfg.AccessTTL)

	// jti lets the backend blacklist a specific issued token on logout.
	var jtiBytes [16]byte
	if _, err := rand.Read(jtiBytes[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("generate jti: %w", err)
	}

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		ID:        hex.EncodeToString(jtiBytes[:]),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(cfg.JWTSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}
