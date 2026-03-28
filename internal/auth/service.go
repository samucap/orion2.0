package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by the service layer. Handlers map these to
// appropriate HTTP status codes without leaking internal details.
var (
	ErrTokenNotFound    = errors.New("refresh token not found")
	ErrTokenRevoked     = errors.New("refresh token has been revoked")
	ErrTokenExpired     = errors.New("refresh token has expired")
	ErrFingerprintMismatch = errors.New("device fingerprint mismatch")
)

const (
	refreshTokenBytes = 32             // 256 bits of entropy
	refreshTokenTTL   = 30 * 24 * time.Hour // 30 days
)

// RefreshService encapsulates all refresh-token business logic.
// The store is injected so tests can substitute an in-memory mock.
type RefreshService struct {
	store     RefreshTokenStore
	jwtSecret []byte
}

// NewRefreshService creates a RefreshService.  It reads JWT_SECRET from
// the environment and panics when the secret is missing (same behaviour as
// the existing middleware.Auth).
func NewRefreshService(store RefreshTokenStore) *RefreshService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		panic("JWT_SECRET environment variable is required for RefreshService")
	}
	return &RefreshService{
		store:     store,
		jwtSecret: []byte(secret),
	}
}

// newRefreshServiceWithSecret is a test-only constructor that accepts the
// JWT secret directly so tests don't depend on environment variables.
func newRefreshServiceWithSecret(store RefreshTokenStore, secret string) *RefreshService {
	return &RefreshService{
		store:     store,
		jwtSecret: []byte(secret),
	}
}

// GenerateOpaqueRefreshToken returns a cryptographically random hex string
// suitable for use as an opaque refresh token (64 hex chars = 256 bits).
func (s *RefreshService) GenerateOpaqueRefreshToken() string {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}
	return hex.EncodeToString(b)
}

// HashRefreshToken returns the hex-encoded SHA-256 digest of a raw token.
// SHA-256 is safe here because the input has 256 bits of entropy, making
// brute-force infeasible even with a fast hash (OWASP, RFC 6819).
func (s *RefreshService) HashRefreshToken(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("empty token")
	}
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:]), nil
}

// ComputeDeviceFingerprint derives a stable per-device identifier from
// request headers.  The fingerprint is a SHA-256 of the concatenation of
// User-Agent, Accept-Language, and X-Forwarded-For.
func (s *RefreshService) ComputeDeviceFingerprint(r *http.Request) string {
	data := r.Header.Get("User-Agent") +
		"|" + r.Header.Get("Accept-Language") +
		"|" + r.Header.Get("X-Forwarded-For")
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// CreateRefreshToken generates a new opaque refresh token for userID,
// persists its SHA-256 hash, and returns the raw token (to be placed in
// an HttpOnly cookie). The caller must never expose the raw value in a
// JSON response body.
func (s *RefreshService) CreateRefreshToken(ctx context.Context, userID int64, fingerprint string) (string, error) {
	raw := s.GenerateOpaqueRefreshToken()

	hash, err := s.HashRefreshToken(raw)
	if err != nil {
		return "", fmt.Errorf("hash refresh token: %w", err)
	}

	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := s.store.InsertRefreshToken(ctx, hash, userID, fingerprint, expiresAt); err != nil {
		return "", fmt.Errorf("persist refresh token: %w", err)
	}

	return raw, nil
}

// ValidateAndRotate validates the incoming raw refresh token, revokes it,
// issues a replacement refresh token and a fresh access JWT, and returns
// the pair. If the token is revoked (replay) or the fingerprint changed,
// ALL of the user's refresh tokens are revoked as a precaution.
func (s *RefreshService) ValidateAndRotate(ctx context.Context, rawRefreshToken string, fingerprint string) (*TokenPair, error) {
	hash, err := s.HashRefreshToken(rawRefreshToken)
	if err != nil {
		return nil, ErrTokenNotFound
	}

	existing, err := s.store.GetByTokenHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("lookup refresh token: %w", err)
	}
	if existing == nil {
		return nil, ErrTokenNotFound
	}

	// Replay detection: a revoked token being presented means a leaked
	// token was already rotated — revoke the entire family.
	if existing.Revoked {
		if revokeErr := s.store.RevokeAllForUser(ctx, existing.UserID); revokeErr != nil {
			slog.Error("failed to revoke token family after replay", "user_id", existing.UserID, "error", revokeErr)
		}
		return nil, ErrTokenRevoked
	}

	if time.Now().After(existing.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	// Fingerprint drift signals a stolen token used from a different device.
	if existing.DeviceFingerprint != fingerprint {
		if revokeErr := s.store.RevokeAllForUser(ctx, existing.UserID); revokeErr != nil {
			slog.Error("failed to revoke tokens after fingerprint mismatch", "user_id", existing.UserID, "error", revokeErr)
		}
		return nil, ErrFingerprintMismatch
	}

	// Revoke the old token before issuing a replacement (rotate).
	if err := s.store.RevokeByID(ctx, existing.ID); err != nil {
		return nil, fmt.Errorf("revoke old token: %w", err)
	}

	// Issue replacement refresh token.
	newRaw, err := s.CreateRefreshToken(ctx, existing.UserID, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("create replacement refresh token: %w", err)
	}

	// Issue fresh access JWT.
	accessToken, expiresAt, err := s.issueAccessToken(existing.UserID)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRaw,
		ExpiresAt:    expiresAt,
	}, nil
}

// RevokeRefreshToken looks up a raw token by its hash and marks it revoked.
func (s *RefreshService) RevokeRefreshToken(ctx context.Context, rawRefreshToken string) error {
	hash, err := s.HashRefreshToken(rawRefreshToken)
	if err != nil {
		return ErrTokenNotFound
	}

	existing, err := s.store.GetByTokenHash(ctx, hash)
	if err != nil {
		return fmt.Errorf("lookup refresh token: %w", err)
	}
	if existing == nil {
		return ErrTokenNotFound
	}

	return s.store.RevokeByID(ctx, existing.ID)
}

// issueAccessToken mints a short-lived HS256 JWT with the same claim
// structure as handlers.generateJWT (sub, iat, exp, jti).  It reads
// JWT_EXPIRY_MINUTES from the environment (default 15).
func (s *RefreshService) issueAccessToken(userID int64) (string, time.Time, error) {
	expiryMinutes := 15
	if v := os.Getenv("JWT_EXPIRY_MINUTES"); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m > 0 {
			expiryMinutes = m
		}
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(expiryMinutes) * time.Minute)

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

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}

	return signed, expiresAt, nil
}
