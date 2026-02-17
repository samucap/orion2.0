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
	liqMin := getParam("liquidityMin", "500")

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
			"&order=%s&volume_min=%s&liquidity_min=%s",
		active, closed, ascending, limit,
		order, volMin, liqMin,
	)

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

	// Load leagues map (cached for performance)
	leaguesBySlug, err := db.QueryAllLeagues(context.Background())
	if err != nil {
		// Log error but don't fail - continue with empty map
		leaguesBySlug = map[string]db.League{}
	}

	// Alias tag slugs -> DB league slugs so EventLeagueSlug resolves them
	tagLeagueAliases := map[string]string{
		"fifa":              "fif",
		"fifa-world-cup":    "fif",
		"2026-fifa-world-cup": "fif",
		"champions-league":  "ucl",
		"uefa":              "ucl",
		"europa-league":     "uel",
		"la-liga":           "lal",
		"serie-a":          "sea",
		"bundesliga":        "bun",
		"ligue-1":           "fl1",
		"cs2":               "csgo",
		"val":               "valorant",
	}
	for tagSlug, dbSlug := range tagLeagueAliases {
		if league, exists := leaguesBySlug[dbSlug]; exists {
			if _, already := leaguesBySlug[tagSlug]; !already {
				leaguesBySlug[tagSlug] = league
			}
		}
	}

	// Load teams by league for sports events (loads ALL teams for each league, not just named ones)
	teamsByName := make(map[string]db.PlyMktTeam)
	leagueSlugs := make(map[string]bool)

	// Tag slug to DB league mapping for known mismatches
	tagToDBLeagues := map[string][]string{
		"fifa":               {"fif"},
		"fifa-world-cup":     {"fif"},
		"2026-fifa-world-cup": {"fif"},
		"champions-league":   {"ucl"},
		"uefa":               {"ucl"},
		"europa-league":      {"uel"},
		"la-liga":            {"lal"},
		"serie-a":           {"sea"},
		"bundesliga":         {"bun"},
		"ligue-1":            {"fl1"},
		"soccer":             {"epl", "ucl", "fif"},
		"cs2":                {"csgo"},
		"val":                {"valorant"},
		"esports":            {"csgo", "lol", "dota2", "rl", "valorant"},
	}

	for _, raw := range rawEvents {
		isSports := false
		for _, tag := range raw.Tags {
			if tag.Slug == "sports" || tag.Slug == "esports" {
				isSports = true
				break
			}
		}
		if !isSports {
			isSports = raw.GameID.String() != "" || raw.Category == "Sports"
		}
		if !isSports {
			continue
		}
		// Collect unique league slugs from sports events
		for _, tag := range raw.Tags {
			if tag.Slug != "" {
				leagueSlugs[tag.Slug] = true
			}
		}
	}

	// Load teams for each detected league (skip generic "sports" tag)
	ctx := context.Background()
	for leagueSlug := range leagueSlugs {
		if leagueSlug == "sports" {
			continue // Skip generic sports tag that pollutes the map
		}

		// Query for the tag slug directly
		leagueTeams, err := db.QueryTeamsByLeague(ctx, leagueSlug)
		if err != nil {
			// Log error but don't fail - continue with empty map
			continue
		}

		// Also query any mapped DB leagues for this tag
		if mappedLeagues, exists := tagToDBLeagues[leagueSlug]; exists {
			for _, mappedLeague := range mappedLeagues {
				mappedTeams, err := db.QueryTeamsByLeague(ctx, mappedLeague)
				if err == nil {
					// Merge mapped teams into leagueTeams
					for k, v := range mappedTeams {
						leagueTeams[k] = v
					}
				}
			}
		}

		// Merge into main map with dual composite keys: tag-slug prefix and DB-league prefix
		for k, v := range leagueTeams {
			teamsByName[k] = v // Keep original DB keys

			// Also add composite key with tag slug as prefix
			// This allows TeamByLabel("fifa", "Argentina") to find "fifa|argentina"
			if strings.Contains(k, "|") {
				// If it's already a composite key, replace the league part with tag slug
				parts := strings.SplitN(k, "|", 2)
				if len(parts) == 2 {
					tagCompositeKey := leagueSlug + "|" + parts[1]
					teamsByName[tagCompositeKey] = v
				}
			} else {
				// For plain keys, add composite key with tag slug
				tagCompositeKey := leagueSlug + "|" + k
				teamsByName[tagCompositeKey] = v
			}
		}
	}

	// Collect team names from sports/esports events (same condition as classification + esports tag)
	var teamNames []string
	for _, raw := range rawEvents {
		isSports := false
		for _, tag := range raw.Tags {
			if tag.Slug == "sports" || tag.Slug == "esports" {
				isSports = true
				break
			}
		}
		if !isSports {
			isSports = raw.GameID.String() != "" || raw.Category == "Sports"
		}
		if !isSports {
			continue
		}
		// Collect from groupItemTitle, raw.Teams, and outcome labels (trimmed)
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
	// Dedupe by normalized key (case-insensitive, UTF-8-safe) so DB and TeamByLabel match
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

	// Add suffix variants for multi-word names (e.g., "Colorado Avalanche" -> also query "Avalanche")
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

	// Query additional teams by name (supplement league-based loading)
	nameBasedTeams, err := db.QueryTeamsByNamesBatched(context.Background(), teamNames, 250)
	if err != nil {
		// Log error but don't fail - continue with empty map
		nameBasedTeams = map[string]db.PlyMktTeam{}
	}
	// Merge name-based teams (these take precedence over league-based ones for same keys)
	for k, v := range nameBasedTeams {
		teamsByName[k] = v
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
