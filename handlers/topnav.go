package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/samucap/orion2.0/internal/db"
)

// RelatedItem represents a related navigation item
type RelatedItem struct {
	Label        string  `json:"label"`
	Slug         string  `json:"slug"`
	TotalVol     float64 `json:"total_vol"`
	TotalVol24hr float64 `json:"total_vol_24hr"`
	TotalLiq     float64 `json:"total_liq"`
	TotalMarkets int     `json:"total_markets"`
}

// NavItem represents a navigation item
type NavItem struct {
	Label        string        `json:"label"`
	Slug         string        `json:"slug"`
	Related      []RelatedItem `json:"related"`
	TotalVol     float64       `json:"total_vol"`
	TotalVol24hr float64       `json:"total_vol_24hr"`
	TotalLiq     float64       `json:"total_liq"`
	TotalMarkets int           `json:"total_markets"`
}

type GetTopNavRequest struct {
	Cat string `json:"cat" validate:"omitempty,alphanum"`
}

// GetTopNav handles GET /top-nav endpoint
func GetTopNav(w http.ResponseWriter, r *http.Request, req GetTopNavRequest) {
	// Get database connection from context (set in main)
	// For now, we'll use stubbed data, but the structure is ready for real DB queries

	cat := req.Cat
	navItems, err := db.QueryTopNav(r.Context(), cat)
	if err != nil {
		// Log the actual error for debugging
		slog.Error("Failed to query top navigation", "error", err)
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(navItems); err != nil {
		// This should rarely happen, but handle it gracefully
		http.Error(w, `{"error":"Failed to encode response"}`, http.StatusInternalServerError)
		return
	}
}
