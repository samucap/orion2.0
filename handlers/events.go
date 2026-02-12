package handlers

import (
	"io"
	"net/http"
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
	// TODO: middleware to get required params from wherever url body and stop if required not present
	// Fetch events from Polymarket API
	params := r.URL.Query()
	currCat := params.Get("cat")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	gammaBase := `https://gamma-api.polymarket.com`
	evPath := `/events?closed=false&active=true&include_chat=false&ascending=false&limit=50&order=volume24hr&related_tags=true`
	if currCat != "" {
		evPath += "&tag_id=" + currCat
	}

	resp, err := http.Get(gammaBase + evPath)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch events from external API"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Check if the API request was successful
	if resp.StatusCode != http.StatusOK {
		http.Error(w, `{"error":"External API returned error"}`, http.StatusInternalServerError)
		return
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"Failed to read response from external API"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Write the raw JSON response from Polymarket API
	if _, err := w.Write(body); err != nil {
		// This should rarely happen, but handle it gracefully
		http.Error(w, `{"error":"Failed to write response"}`, http.StatusInternalServerError)
		return
	}
}
