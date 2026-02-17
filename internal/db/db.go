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
	Label string `json:"label"`
	Slug  string `json:"slug"`
	ID    string `json:"id"`
}

// NavItem represents a navigation item (matching handlers.NavItem)
type NavItem struct {
	Label   string        `json:"label"`
	Slug    string        `json:"slug"`
	ID      string        `json:"id"`
	Related []RelatedItem `json:"related"`
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
func QueryTopNav(ctx context.Context) ([]NavItem, error) {
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

	q := `SELECT
		t.id,
		t.label,
		t.slug,
		COALESCE(
			jsonb_agg(
				jsonb_build_object('id', rt.id, 'label', rt.label, 'slug', rt.slug)
			) FILTER (WHERE rt.id IS NOT NULL),
			'[]'::jsonb
		) AS related
	FROM tags AS t
	LEFT JOIN tags AS rt ON rt.parent_tag_id = t.id
	WHERE t.parent_tag_id = '102982'
	GROUP BY t.id, t.label, t.slug
	ORDER BY t.id ASC`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query navigation items: %w", err)
	}
	defer rows.Close()

	var navItems []NavItem
	for rows.Next() {
		var item NavItem
		var relatedJSON []byte

		err := rows.Scan(&item.ID, &item.Label, &item.Slug, &relatedJSON)
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

// QueryTeamsByNames returns a map of teams keyed by lowercase name, abbreviation, and alias
// for flexible team name matching across different sports events
func QueryTeamsByNames(ctx context.Context, names []string) (map[string]PlyMktTeam, error) {
	return QueryTeamsByNamesAndLeagues(ctx, names, nil)
}

// QueryTeamsByNamesBatched runs QueryTeamsByNames in chunks and merges results.
// Use when the name list is large to keep ANY($1) array size bounded.
func QueryTeamsByNamesBatched(ctx context.Context, names []string, chunkSize int) (map[string]PlyMktTeam, error) {
	if chunkSize <= 0 {
		chunkSize = 250
	}
	merged := make(map[string]PlyMktTeam)
	for i := 0; i < len(names); i += chunkSize {
		end := i + chunkSize
		if end > len(names) {
			end = len(names)
		}
		chunk := names[i:end]
		m, err := QueryTeamsByNames(ctx, chunk)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged, nil
}

// QueryTeamsByNamesAndLeagues returns a map of teams keyed by lowercase name, abbreviation, and alias
// with optional league filtering for tighter matching
func QueryTeamsByNamesAndLeagues(ctx context.Context, names []string, leagueFilter *string) (map[string]PlyMktTeam, error) {
	if Pool == nil {
		// Return empty map if no database connection (non-fatal)
		return map[string]PlyMktTeam{}, nil
	}

	if len(names) == 0 {
		return map[string]PlyMktTeam{}, nil
	}

	// Build query with optional league filtering
	query := `
		SELECT id, name, league, logo, abbreviation, alias, color
		FROM teams
		WHERE (LOWER(name) = ANY($1)
		   OR LOWER(abbreviation) = ANY($1)
		   OR LOWER(alias) = ANY($1))`
	args := []interface{}{}
	argIndex := 2

	// Normalize names for case-insensitive, UTF-8-safe matching
	normalizedNames := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, name := range names {
		k := NormalizeTeamKey(name)
		if k != "" && !seen[k] {
			seen[k] = true
			normalizedNames = append(normalizedNames, k)
		}
	}
	if len(normalizedNames) == 0 {
		return map[string]PlyMktTeam{}, nil
	}
	args = append(args, normalizedNames)

	// Add league filter if provided
	if leagueFilter != nil {
		query += fmt.Sprintf(" AND LOWER(league) = $%d", argIndex)
		args = append(args, NormalizeTeamKey(*leagueFilter))
		argIndex++
	}

	rows, err := Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query teams: %w", err)
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

	return leaguesMap, nil
}
