// Package auth implements opaque refresh token issuance, rotation, and
// revocation. Refresh tokens are stored as SHA-256 hashes (safe for
// high-entropy random tokens) and rotated on every use to limit the
// window of compromise.
//
// SQL migration (run scripts/create_refresh_tokens_table.sql):
//
//	CREATE TABLE IF NOT EXISTS refresh_tokens (
//	    id                  SERIAL PRIMARY KEY,
//	    token_hash          TEXT UNIQUE NOT NULL,
//	    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
//	    expires_at          TIMESTAMPTZ NOT NULL,
//	    revoked             BOOLEAN DEFAULT FALSE,
//	    device_fingerprint  TEXT NOT NULL,
//	    created_at          TIMESTAMPTZ DEFAULT NOW(),
//	    updated_at          TIMESTAMPTZ DEFAULT NOW()
//	);
//	CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
//	CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshToken maps a single row in the refresh_tokens table.
type RefreshToken struct {
	ID                int64
	TokenHash         string
	UserID            int64
	ExpiresAt         time.Time
	Revoked           bool
	DeviceFingerprint string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// TokenPair is returned by ValidateAndRotate: a fresh access JWT plus
// the raw opaque refresh token that must be set as an HttpOnly cookie.
type TokenPair struct {
	AccessToken  string
	RefreshToken string // raw opaque value — never expose in JSON
	ExpiresAt    time.Time
}

// RefreshTokenStore abstracts persistence so the service can be tested
// with an in-memory mock.
type RefreshTokenStore interface {
	// InsertRefreshToken persists a new hashed refresh token.
	InsertRefreshToken(ctx context.Context, tokenHash string, userID int64, fingerprint string, expiresAt time.Time) error
	// GetByTokenHash retrieves a refresh token row by its SHA-256 hash.
	GetByTokenHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	// RevokeByID marks a single refresh token as revoked.
	RevokeByID(ctx context.Context, id int64) error
	// RevokeAllForUser revokes every refresh token belonging to a user
	// (used when token reuse or fingerprint mismatch is detected).
	RevokeAllForUser(ctx context.Context, userID int64) error
}

// ---------------------------------------------------------------------------
// Postgres implementation
// ---------------------------------------------------------------------------

// PgRefreshTokenStore implements RefreshTokenStore backed by pgxpool.
type PgRefreshTokenStore struct {
	pool *pgxpool.Pool
}

// NewPgRefreshTokenStore returns a store that operates on the given pool.
func NewPgRefreshTokenStore(pool *pgxpool.Pool) *PgRefreshTokenStore {
	return &PgRefreshTokenStore{pool: pool}
}

// InsertRefreshToken persists a new hashed refresh token row.
func (s *PgRefreshTokenStore) InsertRefreshToken(ctx context.Context, tokenHash string, userID int64, fingerprint string, expiresAt time.Time) error {
	q := `INSERT INTO refresh_tokens (token_hash, user_id, device_fingerprint, expires_at)
	      VALUES ($1, $2, $3, $4)`
	_, err := s.pool.Exec(ctx, q, tokenHash, userID, fingerprint, expiresAt)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// GetByTokenHash looks up a refresh token by its SHA-256 hash.
func (s *PgRefreshTokenStore) GetByTokenHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	q := `SELECT id, token_hash, user_id, expires_at, revoked, device_fingerprint, created_at, updated_at
	      FROM refresh_tokens WHERE token_hash = $1`

	var t RefreshToken
	err := s.pool.QueryRow(ctx, q, tokenHash).Scan(
		&t.ID, &t.TokenHash, &t.UserID, &t.ExpiresAt,
		&t.Revoked, &t.DeviceFingerprint, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get refresh token by hash: %w", err)
	}
	return &t, nil
}

// RevokeByID marks a single refresh token as revoked.
func (s *PgRefreshTokenStore) RevokeByID(ctx context.Context, id int64) error {
	q := `UPDATE refresh_tokens SET revoked = TRUE WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("revoke refresh token %d: %w", id, err)
	}
	return nil
}

// RevokeAllForUser revokes every active refresh token for a user.
func (s *PgRefreshTokenStore) RevokeAllForUser(ctx context.Context, userID int64) error {
	q := `UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND revoked = FALSE`
	_, err := s.pool.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("revoke all tokens for user %d: %w", userID, err)
	}
	return nil
}
