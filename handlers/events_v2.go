package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samucap/orion2.0/internal/cache"
	"github.com/samucap/orion2.0/internal/db"
)

//TODO: need to return market and events objects withtout much manipulation, because now that I think about it,
// data returned can be used for subsequent routes so no need to manipulate it here

// Package-level cache instance (initialized in main.go)
var eventCache cache.Cache

// SetCache sets the global cache instance
func SetCache(c cache.Cache) {
	eventCache = c
}

// ClearCache handles POST /cache/clear endpoint
// Manually clears the in-memory event cache
func ClearCache(w http.ResponseWriter, r *http.Request) {
	if eventCache != nil {
		eventCache.Clear(r.Context())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"cache_cleared"}`))
}

type GetEventsV2Request struct {
	TagID        string `json:"tag_id" validate:"omitempty,numeric"`
	Cat          string `json:"cat" validate:"omitempty,numeric"`
	Order        string `json:"order" validate:"omitempty,alphanum"`
	Ascending    string `json:"ascending" validate:"omitempty,oneof=true false"`
	Limit        string `json:"limit" validate:"omitempty,numeric"`
	Active       string `json:"active" validate:"omitempty,oneof=true false"`
	Closed       string `json:"closed" validate:"omitempty,oneof=true false"`
	Offset       string `json:"offset" validate:"omitempty,numeric"`
	EndDateMax   string `json:"end_date_max" validate:"omitempty"`
	StartDateMin string `json:"start_date_min" validate:"omitempty"`
	SpreadMax    string `json:"spread_max" validate:"omitempty,numeric"`
	RewardMin    string `json:"rewardMin" validate:"omitempty,numeric"`
	VolumeMin    string `json:"volume_min" validate:"omitempty,numeric"`
	LiquidityMin string `json:"liquidity_min" validate:"omitempty,numeric"`
	ExcludeTagID string `json:"exclude_tag_id" validate:"omitempty,numeric"`
}

// GetEventsV2 handles GET /events-v2 endpoint
// Returns events with fixed classification logic and standardized V2 schema for frontend Event Cards
func GetEventsV2(w http.ResponseWriter, r *http.Request, req GetEventsV2Request) {
	// Helper: return param value or fallback default
	getParam := func(val, fallback string) string {
		if val != "" {
			return val
		}
		return fallback
	}

	// tag_id: accept both "tag_id" and legacy "cat" (tag_id takes priority)
	tagID := req.TagID
	if tagID == "" {
		tagID = req.Cat
	}

	// Params with defaults (preserve existing behavior when frontend omits them)
	order := getParam(req.Order, "volume24hr")
	ascending := getParam(req.Ascending, "false")
	limit := getParam(req.Limit, "50")
	active := getParam(req.Active, "true")
	closed := getParam(req.Closed, "false")

	// Optional params — only appended to Gamma URL if provided
	offset := req.Offset
	endDateMax := req.EndDateMax
	startDateMin := req.StartDateMin
	spreadMax := req.SpreadMax
	rewardMin := req.RewardMin
	volMin := req.VolumeMin
	liqMin := req.LiquidityMin
	excludeTagId := req.ExcludeTagID

	// Build Polymarket Gamma API URL
	gammaBase := `https://gamma-api.polymarket.com`
	evPath := fmt.Sprintf(
		"/events?archived=false&include_chat=false&cyom=false&include_template=false"+
			"&active=%s&closed=%s&ascending=%s&limit=%s"+
			"&order=%s",
		active, closed, ascending, limit,
		order,
	)
	if tagID != "" {
		evPath += "&tag_id=" + tagID
		if tagID == "100215" {
			evPath += "&related_tags=true"
		}
	}

	if volMin != "" {
		evPath += "&volume_min=" + volMin
	}
	if liqMin != "" {
		evPath += "&liquidity_min=" + liqMin
	}
	if offset != "" {
		evPath += "&offset=" + offset
	}
	if endDateMax != "" {
		evPath += "&end_date_max=" + endDateMax
	}
	if startDateMin != "" {
		evPath += "&start_date_min=" + startDateMin
	}
	if spreadMax != "" {
		evPath += "&spread_max=" + spreadMax
	}
	if rewardMin != "" {
		evPath += "&reward_min=" + rewardMin
	}
	if excludeTagId != "" {
		evPath += "&exclude_tag_id=" + excludeTagId
	}

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

	ctx := context.Background()

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
