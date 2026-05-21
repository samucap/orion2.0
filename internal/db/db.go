package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TagToDBLeagues maps Polymarket tag slugs to DB league codes.
// Used for team queries that need to expand tags to multiple leagues (e.g., "soccer" -> ["epl", "ucl", "fif"]).
var TagToDBLeagues = map[string][]string{
	"fifa":                {"fif"},
	"fifa-world-cup":      {"fif"},
	"2026-fifa-world-cup": {"fif"},
	"champions-league":    {"ucl"},
	"uefa":                {"ucl"},
	"europa-league":       {"uel"},
	"la-liga":             {"lal"},
	"serie-a":             {"sea"},
	"bundesliga":          {"bun"},
	"ligue-1":             {"fl1"},
	"soccer":              {"epl", "ucl", "fif"},
	"cs2":                 {"csgo"},
	"val":                 {"valorant"},
	"esports":             {"csgo", "lol", "dota2", "rl", "valorant"},
}

// TagLeagueAliases maps single Polymarket tag slugs to single DB league codes.
// Used for league metadata lookups where each tag maps to exactly one league.
var TagLeagueAliases = map[string]string{
	"fifa":                "fif",
	"fifa-world-cup":      "fif",
	"2026-fifa-world-cup": "fif",
	"champions-league":    "ucl",
	"uefa":                "ucl",
	"europa-league":       "uel",
	"la-liga":             "lal",
	"serie-a":             "sea",
	"bundesliga":          "bun",
	"ligue-1":             "fl1",
	"cs2":                 "csgo",
	"val":                 "valorant",
}

// NormalizeTeamKey returns a case-insensitive, UTF-8-safe key for team name lookups.
// Trims space, strips invalid UTF-8 runes, and lowercases so "Oklahoma City Thunder" and
// capitalized or non-UTF-8 inputs match DB teams consistently.
func NormalizeTeamKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r != utf8.RuneError {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// DB represents the database connection pool
var Pool *pgxpool.Pool

// Event represents an event object (matching handlers.Event)
type Event struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	StartDate string `json:"start_date"`
	Location  string `json:"location"`
}

// RelatedItem represents a related navigation item
type RelatedItem struct {
	Label        string  `json:"label"`
	Slug         string  `json:"slug"`
	ID           string  `json:"id"`
	TotalVol     float64 `json:"total_vol"`
	TotalVol24hr float64 `json:"total_vol_24hr"`
	TotalLiq     float64 `json:"total_liq"`
	TotalMarkets int     `json:"total_markets"`
}

// NavItem represents a navigation item (matching handlers.NavItem)
type NavItem struct {
	Label        string        `json:"label"`
	Slug         string        `json:"slug"`
	ID           string        `json:"id"`
	Related      []RelatedItem `json:"related"`
	TotalVol     float64       `json:"total_vol"`
	TotalVol24hr float64       `json:"total_vol_24hr"`
	TotalLiq     float64       `json:"total_liq"`
	TotalMarkets int           `json:"total_markets"`
}

// PlyMktTeam represents a team from the polydata database
type PlyMktTeam struct {
	ID           int
	Name         string
	League       string
	Logo         string
	Abbreviation string
	Alias        string
	Color        string
}

// League represents a league from the leagues table
type League struct {
	Sport           string
	Image           string
	Ordering        string // "home" or "away"
	LogoURLTemplate string // optional: e.g. "https://.../epl/{team}.png" for derived team logos
}

// InitDB initializes the PostgreSQL connection pool
func InitDB() (*pgxpool.Pool, error) {
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}

	// Build connection string
	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, password, host, port, dbname)

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Configure connection pool
	config.MaxConns = 10
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	Pool = pool
	return pool, nil
}

// NOT USED
// QueryEvents returns a list of events (currently stubbed with hardcoded data)
func QueryEvents(ctx context.Context) ([]Event, error) {
	// TODO: Replace with actual database query
	// Example: SELECT id, title, start_date, location FROM events ORDER BY start_date

	return []Event{
		{
			ID:        1,
			Title:     "Tech Summit 2026",
			StartDate: "2026-03-15",
			Location:  "Mexico City",
		},
		{
			ID:        2,
			Title:     "AI Workshop",
			StartDate: "2026-04-10",
			Location:  "Online",
		},
	}, nil
}

// QueryTopNav returns a list of navigation items from the database
func QueryTopNav(ctx context.Context, cat string) ([]NavItem, error) {
	// TODO: Remove this as if DB is not available, the server won't start.
	if Pool == nil {
		// Return mock data for testing when database is not available
		return []NavItem{
			{
				Label: "Home",
				Slug:  "home",
				Related: []RelatedItem{
					{Label: "Welcome", Slug: "welcome"},
					{Label: "Dashboard", Slug: "dashboard"},
				},
			},
			{
				Label: "Events",
				Slug:  "events",
				Related: []RelatedItem{
					{Label: "Workshops", Slug: "workshops"},
					{Label: "Conferences", Slug: "conferences"},
				},
			},
			{
				Label: "About",
				Slug:  "about",
				Related: []RelatedItem{
					{Label: "Team", Slug: "team"},
					{Label: "Contact", Slug: "contact"},
				},
			},
		}, nil
	}

	parentTagID := "102982"
	if cat != "" {
		parentTagID = cat
	}

	q := `SELECT
		t.id,
		t.label,
		t.slug,
		COALESCE(
			jsonb_agg(
				jsonb_build_object(
					'id', rt.id,
					'label', rt.label,
					'slug', rt.slug,
					'total_vol', COALESCE(rt.total_vol, 0),
					'total_vol_24hr', COALESCE(rt.total_vol_24hr, 0),
					'total_liq', COALESCE(rt.total_liq, 0),
					'total_markets', COALESCE(rt.total_markets, 0)
				)
				ORDER BY COALESCE(rt.total_vol_24hr, 0) DESC
			) FILTER (WHERE rt.id IS NOT NULL),
			'[]'::jsonb
		) AS related,
		COALESCE(t.total_vol, 0),
		COALESCE(t.total_vol_24hr, 0),
		COALESCE(t.total_liq, 0),
		COALESCE(t.total_markets, 0)
	FROM tags AS t
	LEFT JOIN tags AS rt ON rt.parent_tag_id = t.id
	WHERE t.parent_tag_id = $1
	GROUP BY t.id, t.label, t.slug, t.total_vol, t.total_vol_24hr, t.total_liq, t.total_markets
	ORDER BY COALESCE(t.total_vol_24hr, 0) DESC`

	rows, err := Pool.Query(ctx, q, parentTagID)
	if err != nil {
		return nil, fmt.Errorf("failed to query navigation items: %w", err)
	}
	defer rows.Close()

	var navItems []NavItem
	for rows.Next() {
		var item NavItem
		var relatedJSON []byte

		err := rows.Scan(&item.ID, &item.Label, &item.Slug, &relatedJSON, &item.TotalVol, &item.TotalVol24hr, &item.TotalLiq, &item.TotalMarkets)
		if err != nil {
			return nil, fmt.Errorf("failed to scan navigation item: %w", err)
		}

		// Parse the JSON array of related items
		if len(relatedJSON) > 0 && string(relatedJSON) != "[]" {
			err = json.Unmarshal(relatedJSON, &item.Related)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal related JSON: %w", err)
			}
		} else {
			item.Related = []RelatedItem{} // Ensure it's an empty array, not nil
		}

		if item.Label == "All" {
			item.Label = "Trending"
		}

		navItems = append(navItems, item)
	}

	bubble := -1
	for i := range navItems {
		if navItems[i].Slug == "all" {
			bubble = i
			break
		}
	}

	for bubble > 0 {
		navItems[bubble], navItems[bubble-1] = navItems[bubble-1], navItems[bubble]
		bubble--
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating navigation items: %w", err)
	}

	return navItems, nil
}

// QueryTeamsByLeague returns a map of teams keyed by lowercase name, abbreviation, and alias
// for teams in leagues that match the given slug using LIKE (e.g., "nhl" matches "nhl", "nhl-playoffs")
func QueryTeamsByLeague(ctx context.Context, leagueSlug string) (map[string]PlyMktTeam, error) {
	if Pool == nil {
		// Return empty map if no database connection (non-fatal)
		return map[string]PlyMktTeam{}, nil
	}

	if leagueSlug == "" {
		return map[string]PlyMktTeam{}, nil
	}

	query := `
		SELECT id, name, league, logo, abbreviation, alias, color
		FROM teams
		WHERE LOWER(league) = LOWER($1)`

	rows, err := Pool.Query(ctx, query, leagueSlug)
	if err != nil {
		return nil, fmt.Errorf("failed to query teams by league: %w", err)
	}
	defer rows.Close()

	teamsMap := make(map[string]PlyMktTeam)
	for rows.Next() {
		var team PlyMktTeam
		err := rows.Scan(&team.ID, &team.Name, &team.League, &team.Logo, &team.Abbreviation, &team.Alias, &team.Color)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}

		// Add the team to the map with normalized keys for case-insensitive, UTF-8-safe lookup
		// Composite keys (league|name) for league-aware lookup; simple keys as fallback
		leagueKey := NormalizeTeamKey(team.League)
		if team.Name != "" {
			nameKey := NormalizeTeamKey(team.Name)
			teamsMap[nameKey] = team
			if leagueKey != "" {
				teamsMap[leagueKey+"|"+nameKey] = team
			}
		}
		if team.Abbreviation != "" {
			abbrevKey := NormalizeTeamKey(team.Abbreviation)
			teamsMap[abbrevKey] = team
			if leagueKey != "" {
				teamsMap[leagueKey+"|"+abbrevKey] = team
			}
		}
		if team.Alias != "" {
			aliasKey := NormalizeTeamKey(team.Alias)
			teamsMap[aliasKey] = team
			if leagueKey != "" {
				teamsMap[leagueKey+"|"+aliasKey] = team
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating teams: %w", err)
	}

	return teamsMap, nil
}

// QueryLeagueBySport returns league metadata for a specific sport slug
func QueryLeagueBySport(ctx context.Context, sportSlug string) (*League, error) {
	if Pool == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	q := `
		SELECT sport, image, ordering, COALESCE(logo_url_template, '')
		FROM leagues
		WHERE sport = $1
	`

	row := Pool.QueryRow(ctx, q, sportSlug)

	var league League
	err := row.Scan(&league.Sport, &league.Image, &league.Ordering, &league.LogoURLTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to query league for sport %s: %w", sportSlug, err)
	}

	return &league, nil
}

// QueryAllLeagues returns a map of all leagues keyed by sport slug
func QueryAllLeagues(ctx context.Context) (map[string]League, error) {
	if Pool == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	q := `SELECT sport, image, ordering, COALESCE(logo_url_template, '') FROM leagues`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query leagues: %w", err)
	}
	defer rows.Close()

	leaguesMap := make(map[string]League)
	for rows.Next() {
		var league League
		err := rows.Scan(&league.Sport, &league.Image, &league.Ordering, &league.LogoURLTemplate)
		if err != nil {
			return nil, fmt.Errorf("failed to scan league: %w", err)
		}

		leaguesMap[league.Sport] = league
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating leagues: %w", err)
	}

	// Alias tag slugs -> DB league slugs so EventLeagueSlug resolves them and knows the logo template
	for tagSlug, dbSlug := range TagLeagueAliases {
		if league, exists := leaguesMap[dbSlug]; exists {
			if _, already := leaguesMap[tagSlug]; !already {
				leaguesMap[tagSlug] = league
			}
		}
	}

	return leaguesMap, nil
}

// QuerySportsTagSlugs returns a set of tag slugs whose parent_tag_id = 1
// (the "Sports" root tag). Used to identify sports events from their tags
// without hardcoding league/sport slugs in the handler.
func QuerySportsTagSlugs(ctx context.Context) (map[string]bool, error) {
	if Pool == nil {
		return map[string]bool{}, nil
	}

	q := `SELECT slug FROM tags WHERE parent_tag_id = '1' AND slug IS NOT NULL AND slug != ''`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query sports tag slugs: %w", err)
	}
	defer rows.Close()

	slugs := make(map[string]bool)
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("failed to scan sports tag slug: %w", err)
		}
		slugs[slug] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sports tag slugs: %w", err)
	}

	return slugs, nil
}

// QueryTeamsResolved resolves team names using a three-tier approach:
// 1. Exact matches on teams.name, teams.abbreviation, teams.alias
// 2. Alias table matches via team_aliases
// 3. pg_trgm word_similarity fallback for close matches
// Returns map[string]PlyMktTeam keyed by both "league|name" and "name"
func QueryTeamsResolved(ctx context.Context, labels []string, leagues []string) (map[string]PlyMktTeam, error) {
	if Pool == nil {
		return map[string]PlyMktTeam{}, nil
	}

	if len(labels) == 0 || len(leagues) == 0 {
		return map[string]PlyMktTeam{}, nil
	}

	// Normalize leagues and expand to mapped DB leagues
	normalizedLeagues := make([]string, 0, len(leagues))
	seen := make(map[string]bool)

	for _, l := range leagues {
		k := strings.ToLower(strings.TrimSpace(l))
		if k == "" {
			continue
		}

		if !seen[k] {
			seen[k] = true
			normalizedLeagues = append(normalizedLeagues, k)
		}

		// Map and include expanded DB leagues
		if mapped, ok := TagToDBLeagues[k]; ok {
			for _, m := range mapped {
				if !seen[m] {
					seen[m] = true
					normalizedLeagues = append(normalizedLeagues, m)
				}
			}
		}
	}

	if len(normalizedLeagues) == 0 {
		return map[string]PlyMktTeam{}, nil
	}

	query := `
		WITH normalized_labels AS (
			SELECT LOWER(TRIM(label)) as norm_label, label as orig_label
			FROM unnest($1::text[]) as label
		),
		exact AS (
			SELECT t.*, nl.orig_label as query_label
			FROM teams t
			CROSS JOIN normalized_labels nl
			WHERE (LOWER(t.name) = nl.norm_label
			   OR LOWER(COALESCE(t.abbreviation, '')) = nl.norm_label
			   OR LOWER(COALESCE(t.alias, '')) = nl.norm_label)
			  AND LOWER(t.league) = ANY($2)
		),
		alias_match AS (
			SELECT t.*, nl.orig_label as query_label
			FROM teams t
			JOIN team_aliases ta ON ta.team_id = t.id
			CROSS JOIN normalized_labels nl
			WHERE LOWER(ta.alias_name) = nl.norm_label
			  AND LOWER(t.league) = ANY($2)
			  AND nl.orig_label NOT IN (SELECT query_label FROM exact)
		),
		fuzzy AS (
			SELECT DISTINCT ON (nl.orig_label) t.*, nl.orig_label as query_label
			FROM teams t
			CROSS JOIN normalized_labels nl
			WHERE word_similarity(nl.norm_label, LOWER(t.name)) > 0.3
			  AND LOWER(t.league) = ANY($2)
			  AND nl.orig_label NOT IN (SELECT query_label FROM exact)
			  AND nl.orig_label NOT IN (SELECT query_label FROM alias_match)
			ORDER BY nl.orig_label, word_similarity(nl.norm_label, LOWER(t.name)) DESC
		),
		combined AS (
			SELECT * FROM exact
			UNION ALL
			SELECT * FROM alias_match
			UNION ALL
			SELECT * FROM fuzzy
		)
		SELECT DISTINCT c.id, c.name, c.league, c.logo, c.abbreviation, c.alias, c.color, c.query_label
		FROM combined c
	`

	rows, err := Pool.Query(ctx, query, labels, normalizedLeagues)
	if err != nil {
		return nil, fmt.Errorf("failed to query teams resolved: %w", err)
	}
	defer rows.Close()

	teamsMap := make(map[string]PlyMktTeam)
	tagToDbMappings := make(map[string]string) // Needed for duplicating keys

	// Build reverse mapping for tag->db league keys
	for tag, dbLeagues := range TagToDBLeagues {
		for _, dbLeague := range dbLeagues {
			tagToDbMappings[dbLeague] = tag
		}
	}

	for rows.Next() {
		var team PlyMktTeam
		var queryLabel string
		err := rows.Scan(&team.ID, &team.Name, &team.League, &team.Logo, &team.Abbreviation, &team.Alias, &team.Color, &queryLabel)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}

		// Store under multiple key formats for compatibility
		leagueKey := NormalizeTeamKey(team.League)
		queryKey := NormalizeTeamKey(queryLabel)
		tagSlugForLeague := tagToDbMappings[team.League] // e.g. "fif" -> "fifa"

		teamsMap[queryKey] = team
		if leagueKey != "" {
			teamsMap[leagueKey+"|"+queryKey] = team
			if tagSlugForLeague != "" {
				teamsMap[tagSlugForLeague+"|"+queryKey] = team // Duplicate for the tag
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating teams: %w", err)
	}

	return teamsMap, nil
}
