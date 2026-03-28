-- Enable pg_trgm extension for fuzzy text matching
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Create team_aliases table for known name mappings (e.g., "Man United" -> "Manchester United FC")
CREATE TABLE IF NOT EXISTS team_aliases (
    id SERIAL PRIMARY KEY,
    team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    alias_name TEXT NOT NULL,
    UNIQUE(alias_name)
);

-- Indexes for fast lookups
CREATE INDEX IF NOT EXISTS idx_team_aliases_name ON team_aliases (LOWER(alias_name));
CREATE INDEX IF NOT EXISTS idx_team_aliases_name_trgm ON team_aliases USING GIN (LOWER(alias_name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_teams_name_trgm ON teams USING GIN (LOWER(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_teams_alias_trgm ON teams USING GIN (LOWER(COALESCE(alias, '')) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_teams_abbrev_trgm ON teams USING GIN (LOWER(COALESCE(abbreviation, '')) gin_trgm_ops);