package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
}

// NavItem represents a navigation item (matching handlers.NavItem)
type NavItem struct {
	Label   string        `json:"label"`
	Slug    string        `json:"slug"`
	Related []RelatedItem `json:"related"`
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
		t.label,
		t.slug,
		COALESCE(
			jsonb_agg(
				jsonb_build_object('label', rt.label, 'slug', rt.slug)
			) FILTER (WHERE rt.id IS NOT NULL),
			'[]'::jsonb
		) AS related
	FROM tags AS t
	LEFT JOIN tags AS rt ON rt.parent_tag_id = t.id
	WHERE t.parent_tag_id = '102982'
	GROUP BY t.id, t.label, t.slug
	ORDER BY t.id ASC`
	//q := `SELECT
	//	t.label,
	//	t.slug,
	//	COALESCE(
	//		ARRAY_AGG(rt.slug) FILTER (WHERE rt.slug IS NOT NULL),
	//		ARRAY[]::TEXT[]
	//	) AS related
	//FROM tags AS t
	//LEFT JOIN tags AS rt ON rt.parent_tag_id = t.id
	//WHERE t.parent_tag_id = '102982'
	//GROUP BY t.id, t.label, t.slug
	//ORDER BY t.id ASC`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query navigation items: %w", err)
	}
	defer rows.Close()

	var navItems []NavItem
	for rows.Next() {
		var item NavItem
		var relatedJSON []byte

		err := rows.Scan(&item.Label, &item.Slug, &relatedJSON)
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
