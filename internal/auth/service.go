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
	"time"
)

// Sentinel errors returned by the service layer. Handlers map these to
// appropriate HTTP status codes without leaking internal details.
var (
	ErrTokenNotFound       = errors.New("refresh token not found")
	ErrTokenRevoked        = errors.New("refresh token has been revoked")
	ErrTokenExpired        = errors.New("refresh token has expired")
	ErrFingerprintMismatch = errors.New("device fingerprint mismatch")
)

const refreshTokenBytes = 32 // 256 bits of entropy

// RefreshService encapsulates all refresh-token business logic.
// The store is injected so tests can substitute an in-memory mock.
type RefreshService struct {
	store    RefreshTokenStore
	cfg      Config
	userRole func(ctx context.Context, userID int64) (string, error)
}

// SetUserRole supplies the database role used when a refresh mints a new
// access token. When unset, rotation keeps the previous behavior: role
// "user" and a sliding RefreshTTL. Production sets this from users.role.
func (s *RefreshService) SetUserRole(fn func(ctx context.Context, userID int64) (string, error)) {
	s.userRole = fn
}

// NewRefreshService creates a RefreshService that signs access tokens and
// sets refresh-token lifetimes from cfg (see LoadConfig).
func NewRefreshService(store RefreshTokenStore, cfg Config) *RefreshService {
	return &RefreshService{store: store, cfg: cfg}
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

// ComputeDeviceFingerprint derives a stable per-browser identifier from
// User-Agent and Accept-Language. The client IP is excluded on purpose: it
// changes many times a day for legitimate users (mobile handoffs, carrier
// NAT, IPv6 privacy addresses), and a mismatch revokes every session.
func (s *RefreshService) ComputeDeviceFingerprint(r *http.Request) string {
	data := r.Header.Get("User-Agent") + "|" + r.Header.Get("Accept-Language")
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

	expiresAt := time.Now().Add(s.cfg.RefreshTTL)
	if err := s.store.InsertRefreshToken(ctx, hash, userID, fingerprint, expiresAt); err != nil {
		return "", fmt.Errorf("persist refresh token: %w", err)
	}

	return raw, nil
}

// CreateRefreshTokenUntil persists a refresh token that dies at expiresAt.
// Demo sessions use this so later rotations can copy the same deadline.
func (s *RefreshService) CreateRefreshTokenUntil(ctx context.Context, userID int64, fingerprint string, expiresAt time.Time) (string, error) {
	if !expiresAt.After(time.Now()) {
		return "", ErrTokenExpired
	}

	raw := s.GenerateOpaqueRefreshToken()
	hash, err := s.HashRefreshToken(raw)
	if err != nil {
		return "", fmt.Errorf("hash refresh token: %w", err)
	}
	if err := s.store.InsertRefreshToken(ctx, hash, userID, fingerprint, expiresAt); err != nil {
		return "", fmt.Errorf("persist refresh token: %w", err)
	}
	return raw, nil
}

// ValidateAndRotate atomically claims (revokes) the incoming refresh token,
// issues a replacement refresh token and a fresh access JWT, and returns
// the pair. Two concurrent calls with the same token cannot both succeed.
// If the token was already revoked (replay) or the fingerprint changed,
// ALL of the user's refresh tokens are revoked as a precaution.
func (s *RefreshService) ValidateAndRotate(ctx context.Context, rawRefreshToken string, fingerprint string) (*TokenPair, error) {
	hash, err := s.HashRefreshToken(rawRefreshToken)
	if err != nil {
		return nil, ErrTokenNotFound
	}

	claimed, err := s.store.ClaimActive(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("claim refresh token: %w", err)
	}

	if claimed == nil {
		// Nothing claimed: find out why so replays revoke the family.
		existing, err := s.store.GetByTokenHash(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("lookup refresh token: %w", err)
		}
		if existing == nil {
			return nil, ErrTokenNotFound
		}
		if existing.Revoked {
			// A revoked token being presented means a leaked token was
			// already rotated, so revoke the entire family.
			if revokeErr := s.store.RevokeAllForUser(ctx, existing.UserID); revokeErr != nil {
				slog.Error("failed to revoke token family after replay", "user_id", existing.UserID, "error", revokeErr)
			}
			return nil, ErrTokenRevoked
		}
		return nil, ErrTokenExpired
	}

	// Fingerprint drift signals a stolen token used from a different browser.
	if claimed.DeviceFingerprint != fingerprint {
		if revokeErr := s.store.RevokeAllForUser(ctx, claimed.UserID); revokeErr != nil {
			slog.Error("failed to revoke tokens after fingerprint mismatch", "user_id", claimed.UserID, "error", revokeErr)
		}
		return nil, ErrFingerprintMismatch
	}

	role := RoleUser
	if s.userRole != nil {
		role, err = s.userRole(ctx, claimed.UserID)
		if err != nil {
			return nil, fmt.Errorf("resolve role: %w", err)
		}
		if !ValidRole(role) {
			return nil, fmt.Errorf("resolve role: unknown role %q", role)
		}
	}

	refreshExpiry := time.Now().Add(s.cfg.RefreshTTL)
	var newRaw string
	if role == RoleDemo {
		// Keep the original absolute deadline. A sliding hour would never end
		// while the tab keeps refreshing.
		refreshExpiry = claimed.ExpiresAt
		newRaw, err = s.CreateRefreshTokenUntil(ctx, claimed.UserID, fingerprint, refreshExpiry)
	} else {
		newRaw, err = s.CreateRefreshToken(ctx, claimed.UserID, fingerprint)
	}
	if err != nil {
		return nil, fmt.Errorf("create replacement refresh token: %w", err)
	}

	accessToken, expiresAt, err := IssueAccessToken(s.cfg, claimed.UserID, role)
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}

	return &TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     newRaw,
		ExpiresAt:        expiresAt,
		RefreshExpiresAt: refreshExpiry,
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

// RevokeAllForUser revokes every refresh token for a user (password change).
func (s *RefreshService) RevokeAllForUser(ctx context.Context, userID int64) error {
	return s.store.RevokeAllForUser(ctx, userID)
}
