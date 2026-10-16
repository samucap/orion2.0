package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samucap/orion2.0/internal/cache"
	"github.com/samucap/orion2.0/internal/db"
)

// TODO: need to return market and events objects withtout much manipulation, because now that I think about it,
// data returned can be used for subsequent routes so no need to manipulate it here

// Package-level cache instance (initialized in main.go)
var eventCache cache.Cache

// SetCache sets the global cache instance
func SetCache(c cache.Cache) {
	eventCache = c
}

// ClearCache handles POST /api/cache/clear (requires auth)
// Manually clears the in-memory event cache
func ClearCache(w http.ResponseWriter, r *http.Request) {
	if eventCache != nil {
		eventCache.Clear(r.Context())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"cache_cleared"}`))
}

// GetEventsV2Request is the allow-listed query for GET /api/events-v2.
// IDs, limit, and offset are digits only; amounts may be decimals.
type GetEventsV2Request struct {
	TagID        string `json:"tag_id" validate:"omitempty,number,max=20"`
	Cat          string `json:"cat" validate:"omitempty,number,max=20"`
	Order        string `json:"order" validate:"omitempty,alphanum,max=32"`
	Ascending    string `json:"ascending" validate:"omitempty,oneof=true false"`
	Limit        string `json:"limit" validate:"omitempty,uint_lte=100"`
	Active       string `json:"active" validate:"omitempty,oneof=true false"`
	Closed       string `json:"closed" validate:"omitempty,oneof=true false"`
	Offset       string `json:"offset" validate:"omitempty,uint_lte=10000"`
	EndDateMax   string `json:"end_date_max" validate:"omitempty,max=35,date_or_rfc3339"`
	StartDateMin string `json:"start_date_min" validate:"omitempty,max=35,date_or_rfc3339"`
	SpreadMax    string `json:"spread_max" validate:"omitempty,numeric,max=20"`
	RewardMin    string `json:"rewardMin" validate:"omitempty,numeric,max=20"`
	VolumeMin    string `json:"volume_min" validate:"omitempty,numeric,max=20"`
	LiquidityMin string `json:"liquidity_min" validate:"omitempty,numeric,max=20"`
	ExcludeTagID string `json:"exclude_tag_id" validate:"omitempty,number,max=20"`
}

const (
	gammaEventsURL = "https://gamma-api.polymarket.com/events"
	// maxGammaResponseBytes bounds memory used per upstream response.
	maxGammaResponseBytes = 10 << 20
)

// gammaClient bounds every upstream call; http.DefaultClient has no timeout.
var gammaClient = &http.Client{Timeout: 10 * time.Second}

// BuildGammaEventsURL builds the Polymarket Gamma events URL from a validated
// request. Values are encoded with url.Values, so no input can add or
// override query parameters.
func BuildGammaEventsURL(req GetEventsV2Request) string {
	orDefault := func(val, fallback string) string {
		if val != "" {
			return val
		}
		return fallback
	}

	q := url.Values{}
	q.Set("archived", "false")
	q.Set("include_chat", "false")
	q.Set("cyom", "false")
	q.Set("include_template", "false")
	q.Set("active", orDefault(req.Active, "true"))
	q.Set("closed", orDefault(req.Closed, "false"))
	q.Set("ascending", orDefault(req.Ascending, "false"))
	q.Set("limit", orDefault(req.Limit, "50"))
	q.Set("order", orDefault(req.Order, "volume24hr"))

	// tag_id: accept both "tag_id" and legacy "cat" (tag_id takes priority)
	tagID := orDefault(req.TagID, req.Cat)
	if tagID != "" {
		q.Set("tag_id", tagID)
		if tagID == "100215" {
			q.Set("related_tags", "true")
		}
	}

	optional := []struct{ key, val string }{
		{"volume_min", req.VolumeMin},
		{"liquidity_min", req.LiquidityMin},
		{"offset", req.Offset},
		{"end_date_max", req.EndDateMax},
		{"start_date_min", req.StartDateMin},
		{"spread_max", req.SpreadMax},
		{"reward_min", req.RewardMin},
		{"exclude_tag_id", req.ExcludeTagID},
	}
	for _, p := range optional {
		if p.val != "" {
			q.Set(p.key, p.val)
		}
	}

	return gammaEventsURL + "?" + q.Encode()
}

// GetEventsV2 handles GET /events-v2 endpoint
// Returns events with fixed classification logic and standardized V2 schema for frontend Event Cards
func GetEventsV2(w http.ResponseWriter, r *http.Request, req GetEventsV2Request) {
	ctx := r.Context()

	writeUpstreamError := func(status int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(`{"error":"Failed to fetch events from external API"}`))
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, BuildGammaEventsURL(req), nil)
	if err != nil {
		slog.Error("Failed to build Gamma request", "error", err)
		writeUpstreamError(http.StatusInternalServerError)
		return
	}

	resp, err := gammaClient.Do(upstreamReq)
	if err != nil {
		slog.Error("Gamma request failed", "error", err)
		writeUpstreamError(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Gamma returned non-200", "status", resp.StatusCode)
		writeUpstreamError(http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read Gamma response", "error", err)
		writeUpstreamError(http.StatusBadGateway)
		return
	}

	// Parse into typed structs
	var rawEvents []RawGammaEvent
	if err := json.Unmarshal(body, &rawEvents); err != nil {
		slog.Error("Failed to parse Gamma response", "error", err)
		writeUpstreamError(http.StatusBadGateway)
		return
	}

	// Load leagues map (cached for performance)
	leaguesBySlug, err := db.QueryAllLeagues(ctx)
	if err != nil {
		leaguesBySlug = map[string]db.League{}
	}

	// Identify sports events and collect team names + league slugs in a single pass
	var teamLabels []string
	leagueSlugs := make(map[string]bool)

	for _, raw := range rawEvents {
		if !IsSportsEvent(raw) {
			continue
		}

		// Collect unique league slugs
		for _, tag := range raw.Tags {
			if tag.Slug != "" && tag.Slug != "sports" && tag.Slug != "esports" {
				leagueSlugs[tag.Slug] = true
			}
		}

		// Collect team labels from sports events
		for _, market := range raw.Markets {
			if s := strings.TrimSpace(market.GroupItemTitle); s != "" {
				teamLabels = append(teamLabels, s)
			}
		}
		for _, team := range raw.Teams {
			if s := strings.TrimSpace(team.Name); s != "" {
				teamLabels = append(teamLabels, s)
			}
		}
		for _, market := range raw.Markets {
			if market.OutcomesRaw != "" {
				var outcomes []string
				if err := json.Unmarshal([]byte(market.OutcomesRaw), &outcomes); err == nil {
					for _, outcome := range outcomes {
						s := strings.TrimSpace(outcome)
						if s != "" && s != "Yes" && s != "No" {
							teamLabels = append(teamLabels, s)
						}
					}
				}
			}
		}
	}

	// Deduplicate team labels by normalized key (similar to old system)
	uniqueLabels := make(map[string]string)
	for _, label := range teamLabels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		key := db.NormalizeTeamKey(label)
		if key != "" && uniqueLabels[key] == "" {
			uniqueLabels[key] = label
		}
	}

	dedupedLabels := make([]string, 0, len(uniqueLabels))
	for _, label := range uniqueLabels {
		dedupedLabels = append(dedupedLabels, label)
	}

	// Convert to slices and resolve teams with the new unified query
	var targetLeagues []string
	for slug := range leagueSlugs {
		targetLeagues = append(targetLeagues, slug)
	}

	teamsByName, err := db.QueryTeamsResolved(ctx, dedupedLabels, targetLeagues)
	if err != nil {
		teamsByName = map[string]db.PlyMktTeam{}
	}

	// Sort sports markets so moneyline appears first (ensures Outcomes[0] uses moneyline)
	for i := range rawEvents {
		if IsSportsEvent(rawEvents[i]) {
			sortMarketsMoneylineFirst(rawEvents[i].Markets)
		}
	}

	// Transform each event using V2 logic with caching
	v2Events := make([]V2Event, 0, len(rawEvents))

	for _, raw := range rawEvents {
		// Check cache first
		var v2Event *V2Event
		if eventCache != nil {
			cacheKey := fmt.Sprintf("enriched:%s", raw.ID)
			if cachedData, found := eventCache.Get(ctx, cacheKey); found {
				var cachedEvent V2Event
				if err := json.Unmarshal(cachedData, &cachedEvent); err == nil {
					v2Event = &cachedEvent
				}
			}
		}

		// Transform if not cached
		if v2Event == nil {
			v2Event = TransformEventV2(raw, teamsByName, leaguesBySlug)
			if v2Event == nil {
				continue
			}

			// Cache the result
			if eventCache != nil {
				cacheKey := fmt.Sprintf("enriched:%s", raw.ID)
				if eventData, err := json.Marshal(v2Event); err == nil {
					eventCache.Set(ctx, cacheKey, eventData, 10*time.Minute)
				}
			}
		}

		v2Events = append(v2Events, *v2Event)
	}

	// Return V2 response (array of events directly)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v2Events)
}
