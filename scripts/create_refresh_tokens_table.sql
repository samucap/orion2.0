-- Refresh tokens table for opaque token rotation.
-- Tokens are stored as SHA-256 hashes (high-entropy inputs make fast hashing safe).
-- Run AFTER create_users_table.sql.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id                  SERIAL PRIMARY KEY,
    token_hash          TEXT UNIQUE NOT NULL,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked             BOOLEAN DEFAULT FALSE,
    device_fingerprint  TEXT NOT NULL,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);

-- Auto-update updated_at on row changes (reuses the function from create_users_table.sql).
CREATE TRIGGER update_refresh_tokens_updated_at
    BEFORE UPDATE ON refresh_tokens
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
