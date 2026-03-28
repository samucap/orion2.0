package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TokenBlacklistStore defines the persistence operations used to revoke tokens.
// Tests can swap out db.TokenBlacklist with a mock implementation.
type TokenBlacklistStore interface {
	BlacklistToken(ctx context.Context, jti string, userID int64, expiresAt time.Time) error
	IsTokenBlacklisted(ctx context.Context, jti string) (bool, error)
	CleanupExpiredTokens(ctx context.Context) error
}

// PgTokenBlacklistStore is the default TokenBlacklistStore backed by Postgres.
type PgTokenBlacklistStore struct{}

// TokenBlacklist is the active token blacklist store. Override in unit tests.
var TokenBlacklist TokenBlacklistStore = PgTokenBlacklistStore{}

func (PgTokenBlacklistStore) BlacklistToken(ctx context.Context, jti string, userID int64, expiresAt time.Time) error {
	if Pool == nil {
		return fmt.Errorf("database not initialized")
	}
	if jti == "" {
		return fmt.Errorf("jti is required")
	}

	q := `
		INSERT INTO token_blacklist (jti, user_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (jti) DO UPDATE
		SET expires_at = EXCLUDED.expires_at
	`
	_, err := Pool.Exec(ctx, q, jti, userID, expiresAt)
	if err != nil {
		// If users row is missing (shouldn't happen), return a generic error.
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23503" {
			return fmt.Errorf("cannot blacklist token: user not found")
		}
		return fmt.Errorf("failed to blacklist token: %w", err)
	}
	return nil
}

func (PgTokenBlacklistStore) IsTokenBlacklisted(ctx context.Context, jti string) (bool, error) {
	if Pool == nil {
		return false, fmt.Errorf("database not initialized")
	}
	if jti == "" {
		return false, nil
	}

	q := `SELECT 1 FROM token_blacklist WHERE jti = $1 AND expires_at > NOW() LIMIT 1`
	var one int
	err := Pool.QueryRow(ctx, q, jti).Scan(&one)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check token blacklist: %w", err)
	}
	return true, nil
}

func (PgTokenBlacklistStore) CleanupExpiredTokens(ctx context.Context) error {
	if Pool == nil {
		return fmt.Errorf("database not initialized")
	}
	q := `DELETE FROM token_blacklist WHERE expires_at < NOW()`
	_, err := Pool.Exec(ctx, q)
	if err != nil {
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}
	return nil
}

