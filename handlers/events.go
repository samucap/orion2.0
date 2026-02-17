package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetEvents handles GET /events endpoint
// Returns sanitized, render-ready CleanEvent data from Polymarket Gamma API
func GetEvents(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	currCat := params.Get("cat")
	currOrder := params.Get("order")
	if currOrder == "" {
		currOrder = "volume24hr"
	}

	// Build Polymarket API URL
	gammaBase := `https://gamma-api.polymarket.com`
	evPath := `/events?closed=false&active=true&archived=false&include_chat=false&ascending=false&limit=50&cyom=false&include_template=false&liquidity_min=500&volume_min=500`
	if currCat != "" {
		evPath += "&tag_id=" + currCat
		if currCat == "100215" {
			evPath += "&related_tags=true"
		}
	}

	evPath += "&order=" + currOrder

	resp, err := http.Get(gammaBase + evPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Failed to fetch events from external API"}`))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf(`{"error":"External API returned status %d"}`, resp.StatusCode)))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Failed to read response from external API"}`))
		return
	}

	// Parse into typed structs
	var rawEvents []RawGammaEvent
	if err := json.Unmarshal(body, &rawEvents); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(fmt.Sprintf(`{"error":"Invalid response from external API: %s"}`, err.Error())))
		return
	}

	// Transform each event, filtering out zombie events
	cleanEvents := make([]CleanEvent, 0, len(rawEvents))
	for _, raw := range rawEvents {
		clean := TransformEvent(raw)
		if clean != nil {
			cleanEvents = append(cleanEvents, *clean)
		}
	}

	// Return render-ready events
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(cleanEvents)
}
