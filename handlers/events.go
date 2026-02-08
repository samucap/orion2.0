package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/samucap/orion2.0/internal/db"
)

// Event represents an event object
type Event struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	StartDate string `json:"start_date"`
	Location  string `json:"location"`
}

// GetEvents handles GET /events endpoint
func GetEvents(w http.ResponseWriter, r *http.Request) {
	// Get database connection from context (set in main)
	// For now, we'll use stubbed data, but the structure is ready for real DB queries

	events, err := db.QueryEvents(r.Context())
	if err != nil {
		http.Error(w, `{"error":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(events); err != nil {
		// This should rarely happen, but handle it gracefully
		http.Error(w, `{"error":"Failed to encode response"}`, http.StatusInternalServerError)
		return
	}
}
