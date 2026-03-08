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

// GetEventsV2 handles GET /events-v2 endpoint
// Returns events with fixed classification logic and standardized V2 schema for frontend Event Cards
func GetEventsV2(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	// Helper: return param value or fallback default
	getParam := func(key, fallback string) string {
		if v := params.Get(key); v != "" {
			return v
		}
		return fallback
	}

	// tag_id: accept both "tag_id" and legacy "cat" (tag_id takes priority)
	tagID := params.Get("tag_id")
	if tagID == "" {
		tagID = params.Get("cat")
	}

	// Params with defaults (preserve existing behavior when frontend omits them)
	order := getParam("order", "volume24hr")
	ascending := getParam("ascending", "false")
	limit := getParam("limit", "50")
	active := getParam("active", "true")
	closed := getParam("closed", "false")
	volMin := getParam("volumeMin", "500")

	// Optional params — only appended to Gamma URL if provided
	offset := params.Get("offset")
	endDateMax := params.Get("end_date_max")
	startDateMin := params.Get("start_date_min")
	spreadMax := params.Get("spread_max")
	rewardMin := params.Get("rewardMin")

	// Build Polymarket Gamma API URL
	gammaBase := `https://gamma-api.polymarket.com`
	evPath := fmt.Sprintf(
		"/events?archived=false&include_chat=false&cyom=false&include_template=false"+
			"&active=%s&closed=%s&ascending=%s&limit=%s"+
			"&order=%s&volume_min=%s",
		active, closed, ascending, limit,
		order, volMin,
	)
	if liqMin := params.Get("liquidityMin"); liqMin != "" {
		evPath += "&liquidity_min=" + liqMin
	}

	if tagID != "" {
		evPath += "&tag_id=" + tagID
		if tagID == "100215" {
			evPath += "&related_tags=true"
		}
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

	// 1. Get sports tags from DB
	sportsTags, err := db.QuerySportsTagSlugs(ctx)
	if err != nil {
		sportsTags = map[string]bool{"sports": true, "esports": true, "soccer": true, "football": true} // fallback
	}

	// 2. Identify sports events and collect team names AND league slugs in a single pass
	var teamNames []string
	leagueSlugs := make(map[string]bool)

	for _, raw := range rawEvents {
		isSports := false
		if raw.Category == "Sports" || raw.GameID.String() != "" {
			isSports = true
		} else {
			for _, tag := range raw.Tags {
				if sportsTags[tag.Slug] {
					isSports = true
					break
				}
			}
		}

		if !isSports {
			continue
		}

		// Collect unique league slugs
		for _, tag := range raw.Tags {
			if tag.Slug != "" && tag.Slug != "sports" && tag.Slug != "esports" {
				leagueSlugs[tag.Slug] = true
			}
		}

		// Collect names from sports events
		for _, market := range raw.Markets {
			if s := strings.TrimSpace(market.GroupItemTitle); s != "" {
				teamNames = append(teamNames, s)
			}
		}
		for _, team := range raw.Teams {
			if s := strings.TrimSpace(team.Name); s != "" {
				teamNames = append(teamNames, s)
			}
		}
		for _, market := range raw.Markets {
			if market.OutcomesRaw != "" {
				var outcomes []string
				if err := json.Unmarshal([]byte(market.OutcomesRaw), &outcomes); err == nil {
					for _, outcome := range outcomes {
						s := strings.TrimSpace(outcome)
						if s != "" && s != "Yes" && s != "No" {
							teamNames = append(teamNames, s)
						}
					}
				}
			}
		}
	}

	// Convert league map to slice
	var targetLeagues []string
	for slug := range leagueSlugs {
		targetLeagues = append(targetLeagues, slug)
	}

	// 3. Batch query ALL teams in the detected leagues (required for TeamByLabel fuzzy matching)
	teamsByName, err := db.QueryTeamsByLeagues(ctx, targetLeagues)
	if err != nil {
		teamsByName = map[string]db.PlyMktTeam{}
	}

	// 4. Also batch query the specific detected names as a fallback (for cross-league teams)
	// Dedupe by normalized key first
	uniqueByKey := make(map[string]string)
	for _, n := range teamNames {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := db.NormalizeTeamKey(n)
		if key != "" && uniqueByKey[key] == "" {
			uniqueByKey[key] = n
		}
	}
	teamNames = make([]string, 0, len(uniqueByKey))
	for _, n := range uniqueByKey {
		teamNames = append(teamNames, n)
	}

	// Add suffix variants for multi-word names
	var suffixNames []string
	for _, n := range teamNames {
		words := strings.Fields(n)
		for i := 1; i < len(words); i++ {
			suffix := strings.Join(words[i:], " ")
			if suffix != "" {
				suffixNames = append(suffixNames, suffix)
			}
		}
	}
	teamNames = append(teamNames, suffixNames...)

	if nameBasedTeams, err := db.QueryTeamsByNamesBatched(ctx, teamNames, 250); err == nil {
		for k, v := range nameBasedTeams {
			teamsByName[k] = v
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
