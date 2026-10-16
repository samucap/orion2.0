package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/samucap/orion2.0/internal/db"
)

func DashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := db.QueryDashboardStats(r.Context())
	if err != nil {
		writeDashboardErr(w, "stats", err)
		return
	}
	writeDashboardJSON(w, stats)
}

func DashboardTrending(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryTrendingMarkets(r.Context())
	if err != nil {
		writeDashboardErr(w, "trending", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func DashboardActivity(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryActivity(r.Context())
	if err != nil {
		writeDashboardErr(w, "activity", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func DashboardTreemap(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryTreemap(r.Context())
	if err != nil {
		writeDashboardErr(w, "treemap", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func DashboardEdge(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryEdgeBoard(r.Context())
	if err != nil {
		writeDashboardErr(w, "edge", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func DashboardWhales(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryWhales(r.Context())
	if err != nil {
		writeDashboardErr(w, "whales", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func DashboardDepth(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryDepth(r.Context())
	if err != nil {
		writeDashboardErr(w, "depth", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func DashboardCorrelation(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryCorrelation(r.Context())
	if err != nil {
		writeDashboardErr(w, "correlation", err)
		return
	}
	writeDashboardJSON(w, map[string]any{"links": rows})
}

// No cross-venue book is stored. The route exists so the panel can tell a
// failed request from an empty source.
func DashboardArbitrage(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryArbitrage(r.Context())
	if err != nil {
		writeDashboardErr(w, "arbitrage", err)
		return
	}
	writeDashboardJSON(w, rows)
}

func writeDashboardJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("dashboard encode", "error", err)
	}
}

func writeDashboardErr(w http.ResponseWriter, name string, err error) {
	slog.Error("dashboard query", "endpoint", name, "error", err)
	status := http.StatusInternalServerError
	if errors.Is(err, db.ErrNoPool) {
		status = http.StatusServiceUnavailable
	}
	http.Error(w, `{"error":"Internal server error"}`, status)
}
