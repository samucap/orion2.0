-- Create users table for authentication system
-- Run this migration before enabling authentication features

CREATE TABLE IF NOT EXISTS users (
    id          BIGSERIAL NOT NULL PRIMARY KEY,
    email       TEXT NOT NULL UNIQUE,
    pw          TEXT NOT NULL,
    avatar      TEXT NOT NULL DEFAULT '',
    last_login  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for fast email lookups during login
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

-- Index for potential last_login queries (e.g., inactive user cleanup)
CREATE INDEX IF NOT EXISTS idx_users_last_login ON users (last_login);

-- Trigger to automatically update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();