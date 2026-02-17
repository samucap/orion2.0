-- Add optional logo URL template to leagues for derived team logo fallback.
-- Run against your polydata DB.
-- Placeholders: {team} = normalized label slug (e.g. "man-city"); {abbrev} = team abbreviation (e.g. "arg", "ars").
-- League rows must exist (sport = Polymarket tag slug).
--
-- To discover and verify templates from live API + HEAD checks, run:
--   go run ./scripts/polymarket_s3_explore/ -discover-templates
-- Then paste the printed UPDATEs below, or run with -update-db to apply directly (requires POSTGRES_* env).
ALTER TABLE leagues ADD COLUMN IF NOT EXISTS logo_url_template TEXT;

-- Country flags (international sports): S3 uses abbreviation, e.g. country-flags/arg.png
-- Use tag slug that matches your events (e.g. international-sports, fifa). Insert league if missing.
UPDATE leagues SET logo_url_template = 'https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/{abbrev}.png'
WHERE sport IN ('international-sports', 'fifa', 'country-flags');

-- EPL: S3 often uses abbreviation (e.g. epl/ars.png for Arsenal)
UPDATE leagues SET logo_url_template = 'https://polymarket-upload.s3.us-east-2.amazonaws.com/epl/{abbrev}.png'
WHERE sport = 'epl';

-- NBA: common pattern is nba/{team}.png or nba/{abbrev}.png; adjust after verifying S3 paths
UPDATE leagues SET logo_url_template = 'https://polymarket-upload.s3.us-east-2.amazonaws.com/nba/{team}.png'
WHERE sport = 'nba';

-- NHL: common pattern is nhl/{team}.png or nhl/{abbrev}.png; adjust after verifying S3 paths
UPDATE leagues SET logo_url_template = 'https://polymarket-upload.s3.us-east-2.amazonaws.com/nhl/{team}.png'
WHERE sport = 'nhl';

-- UCL / Champions League: adjust path and placeholder after verifying S3
UPDATE leagues SET logo_url_template = 'https://polymarket-upload.s3.us-east-2.amazonaws.com/ucl/{team}.png'
WHERE sport IN ('ucl', 'champions-league');

-- Soccer (generic): adjust path if Polymarket uses a different folder
UPDATE leagues SET logo_url_template = 'https://polymarket-upload.s3.us-east-2.amazonaws.com/soccer/{team}.png'
WHERE sport = 'soccer';
