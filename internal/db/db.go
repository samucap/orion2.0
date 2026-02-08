package db

import (
	"context"
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

// NavItem represents a navigation item (matching handlers.NavItem)
type NavItem struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Order int    `json:"order"`
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

// QueryTopNav returns a list of navigation items (currently stubbed with hardcoded data)
func QueryTopNav(ctx context.Context) ([]NavItem, error) {
	// TODO: Replace with actual database query
	// Example: SELECT label, url, order FROM navigation_items ORDER BY order

	return []NavItem{
		{
			Label: "Home",
			URL:   "/",
			Order: 1,
		},
		{
			Label: "Events",
			URL:   "/events",
			Order: 2,
		},
		{
			Label: "About",
			URL:   "/about",
			Order: 3,
		},
	}, nil
}