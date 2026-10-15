-- Indexes for the navigation query in internal/db/db.go
-- Serves both "children of a parent tag" lookups and the top-N-by-24h-volume
-- ordering, so each parent's related tags are read in order without a sort.
-- The expression must match the query's ORDER BY exactly.
CREATE INDEX IF NOT EXISTS idx_tags_parent_vol24
    ON tags (parent_tag_id, (COALESCE(total_vol_24hr, 0)) DESC);
