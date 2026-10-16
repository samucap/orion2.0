-- Add the authorization role used by access JWTs.
-- Run once on databases created before users.role existed.
-- Refresh tokens do not store a role. They keep user_id, and the server
-- reads users.role when it mints the next access token.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'demo'));
