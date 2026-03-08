package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Layout constants
const (
	LayoutPoll   = "POLL"
	LayoutSports = "SPORTS"
	LayoutBinary = "BINARY"
)

// TransformEvent converts a raw Gamma API event into a clean, render-ready event.
// Returns nil if all markets are zombies (should be dropped).
func TransformEvent(raw RawGammaEvent) *CleanEvent {
	// Step 1: Filter zombie markets
	validMarkets := filterZombieMarkets(raw)
	if len(validMarkets) == 0 {
		return nil
	}

	// Step 2: Classify layout
	layout := classifyLayout(raw, validMarkets)

	// Step 3: Build display data based on layout
	displayData := buildDisplayData(raw, validMarkets, layout)

	// Step 4: Calculate stats from primary market
	stats := calculateStats(validMarkets)

	// Step 5: Build status badge
	statusBadge := buildStatusBadge(raw, stats)

	// Step 6: Build ticker
	ticker := buildTicker(raw, validMarkets, layout)

	return &CleanEvent{
		ID:          raw.ID,
		Title:       raw.Title,
		Ticker:      ticker,
		Layout:      layout,
		Image:       raw.Image,
		IsLive:      raw.Active,
		StatusBadge: statusBadge,
		Stats:       stats,
		DisplayData: displayData,
	}
}

// =============================================================================
// Step 1: Zombie Filter
// =============================================================================

// filterZombieMarkets removes markets that are at extreme prices (zombies)
// unless the event is disputed.
func filterZombieMarkets(raw RawGammaEvent) []RawGammaMarket {
	isDisputed := isEventDisputed(raw)

	validMarkets := make([]RawGammaMarket, 0, len(raw.Markets))
	for _, market := range raw.Markets {
		prices := parseRawPrices(market.OutcomePricesRaw)
		if len(prices) == 0 {
			continue
		}

		// Primary price is index 0 (usually "Yes")
		primaryPrice := prices[0]
		isZombie := primaryPrice >= 0.99 || primaryPrice <= 0.01

		// Keep if: not zombie, OR if disputed
		if !isZombie || isDisputed {
			validMarkets = append(validMarkets, market)
		}
	}

	return validMarkets
}

// isEventDisputed checks if the event has a disputed or answering UMA status.
func isEventDisputed(raw RawGammaEvent) bool {
	status := strings.ToLower(raw.UmaResolutionStatuses)
	return strings.Contains(status, "disputed") || strings.Contains(status, "answering")
}

// =============================================================================
// Step 2: String Parsing Helpers
// =============================================================================

// parseRawPrices parses the JSON string of outcome prices into a float slice.
// Returns empty slice on error.
func parseRawPrices(raw string) []float64 {
	if raw == "" {
		return nil
	}

	// First try to unmarshal as []string (common format)
	var priceStrs []string
	if err := json.Unmarshal([]byte(raw), &priceStrs); err == nil {
		prices := make([]float64, 0, len(priceStrs))
		for _, s := range priceStrs {
			if p, err := strconv.ParseFloat(s, 64); err == nil {
				prices = append(prices, p)
			}
		}
		return prices
	}

	// Try to unmarshal as []float64 (alternative format)
	var prices []float64
	if err := json.Unmarshal([]byte(raw), &prices); err == nil {
		return prices
	}

	return nil
}

// parseRawOutcomes parses the JSON string of outcome labels into a string slice.
// Returns empty slice on error.
func parseRawOutcomes(raw string) []string {
	if raw == "" {
		return nil
	}

	var outcomes []string
	if err := json.Unmarshal([]byte(raw), &outcomes); err != nil {
		return nil
	}
	return outcomes
}

// =============================================================================
// Step 3: Layout Classification
// =============================================================================

// classifyLayout determines the layout type based on event and market properties.
func classifyLayout(raw RawGammaEvent, markets []RawGammaMarket) string {
	// CASE A: POLL / NEGRISK (Politics or negRisk)
	if raw.Category == "Politics" || raw.NegRisk {
		return LayoutPoll
	}

	// CASE B: SPORTS (Sports category or any market has TeamAID)
	if raw.Category == "Sports" {
		return LayoutSports
	}
	for _, m := range markets {
		if m.SportsMarketType != "" {
			return LayoutSports
		}
	}

	// CASE C: BINARY (Default)
	return LayoutBinary
}

// =============================================================================
// Step 4: Build Display Data
// =============================================================================

// buildDisplayData constructs the DisplayData based on layout type.
func buildDisplayData(raw RawGammaEvent, markets []RawGammaMarket, layout string) DisplayData {
	switch layout {
	case LayoutPoll:
		return buildPollDisplayData(raw, markets)
	case LayoutSports:
		return buildSportsDisplayData(raw, markets)
	default:
		return buildBinaryDisplayData(raw, markets)
	}
}

// buildPollDisplayData creates display data for POLL layout.
// Each market represents one candidate, sorted by price DESC.
func buildPollDisplayData(raw RawGammaEvent, markets []RawGammaMarket) DisplayData {
	outcomes := make([]OutcomeObj, 0, len(markets))

	for _, market := range markets {
		prices := parseRawPrices(market.OutcomePricesRaw)
		yesPrice := 0.0
		if len(prices) > 0 {
			yesPrice = prices[0] // "Yes" price
		}

		outcomes = append(outcomes, OutcomeObj{
			Label:    market.GroupItemTitle,
			Price:    yesPrice,
			Image:    market.Image,
			IsWinner: false, // Will be set after sorting
		})
	}

	// Sort by price DESC
	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[i].Price > outcomes[j].Price
	})

	// Mark the first (highest price) as winner
	if len(outcomes) > 0 {
		outcomes[0].IsWinner = true
	}

	return DisplayData{Outcomes: outcomes}
}

// buildSportsDisplayData creates display data for SPORTS layout.
func buildSportsDisplayData(raw RawGammaEvent, markets []RawGammaMarket) DisplayData {
	// Find the main market (highest liquidity)
	var mainMarket *RawGammaMarket
	maxLiquidity := 0.0
	for i := range markets {
		liq := markets[i].Liquidity.Float64()
		if liq > maxLiquidity {
			maxLiquidity = liq
			mainMarket = &markets[i]
		}
	}

	if mainMarket == nil && len(markets) > 0 {
		mainMarket = &markets[0]
	}

	if mainMarket == nil {
		return DisplayData{}
	}

	// Parse prices and outcomes
	prices := parseRawPrices(mainMarket.OutcomePricesRaw)
	outcomes := parseRawOutcomes(mainMarket.OutcomesRaw)

	homePrice := 0.0
	awayPrice := 0.0
	homeLabel := "Home"
	awayLabel := "Away"

	if len(prices) >= 2 {
		homePrice = prices[0]
		awayPrice = prices[1]
	}
	if len(outcomes) >= 2 {
		homeLabel = outcomes[0]
		awayLabel = outcomes[1]
	}

	// Determine favorite (higher price)
	homeFavorite := homePrice >= awayPrice

	matchup := &MatchupObj{
		Home: TeamObj{
			Label:      homeLabel,
			Price:      homePrice,
			IsFavorite: homeFavorite,
			TeamID:     mainMarket.TeamAID,
		},
		Away: TeamObj{
			Label:      awayLabel,
			Price:      awayPrice,
			IsFavorite: !homeFavorite,
			TeamID:     mainMarket.TeamBID,
		},
	}

	return DisplayData{Teams: matchup}
}

// buildBinaryDisplayData creates display data for BINARY layout.
func buildBinaryDisplayData(raw RawGammaEvent, markets []RawGammaMarket) DisplayData {
	if len(markets) == 0 {
		return DisplayData{}
	}

	market := markets[0]
	prices := parseRawPrices(market.OutcomePricesRaw)
	outcomeLabels := parseRawOutcomes(market.OutcomesRaw)

	// Default labels if not available
	if len(outcomeLabels) == 0 {
		outcomeLabels = []string{"Yes", "No"}
	}

	outcomes := make([]OutcomeObj, 0, len(outcomeLabels))
	for i, label := range outcomeLabels {
		price := 0.0
		if i < len(prices) {
			price = prices[i]
		}

		outcomes = append(outcomes, OutcomeObj{
			Label:    label,
			Price:    price,
			Image:    market.Image,
			IsWinner: i == 0, // "Yes" is typically the main outcome
		})
	}

	return DisplayData{Outcomes: outcomes}
}

// =============================================================================
// Step 5: Calculate Stats
// =============================================================================

// calculateStats computes trading metrics from the markets.
func calculateStats(markets []RawGammaMarket) EventStats {
	if len(markets) == 0 {
		return EventStats{}
	}

	// Find primary market (highest liquidity)
	var primaryMarket *RawGammaMarket
	maxLiquidity := 0.0
	totalVolume := 0.0

	for i := range markets {
		totalVolume += markets[i].Volume24hr.Float64()
		liq := markets[i].Liquidity.Float64()
		if liq > maxLiquidity {
			maxLiquidity = liq
			primaryMarket = &markets[i]
		}
	}

	if primaryMarket == nil {
		primaryMarket = &markets[0]
	}

	// Calculate spread in basis points
	spreadBP := int((primaryMarket.BestAsk.Float64() - primaryMarket.BestBid.Float64()) * 10000)
	if spreadBP < 0 {
		spreadBP = 0
	}

	// Determine whale action signal
	isWhaleAction := totalVolume > 100000 && spreadBP < 200

	return EventStats{
		VolumeUSD:     formatVolumeUSD(totalVolume),
		SpreadBP:      spreadBP,
		IsWhaleAction: isWhaleAction,
	}
}

// formatVolumeUSD converts a volume float to a human-readable string.
func formatVolumeUSD(volume float64) string {
	switch {
	case volume >= 1000000:
		return fmt.Sprintf("$%.1fM", volume/1000000)
	case volume >= 1000:
		return fmt.Sprintf("$%.0fK", volume/1000)
	default:
		return fmt.Sprintf("$%.0f", volume)
	}
}

// =============================================================================
// Step 6: Build Status Badge
// =============================================================================

// buildStatusBadge determines the status badge based on event state.
func buildStatusBadge(raw RawGammaEvent, stats EventStats) string {
	// Check for disputed status first
	if isEventDisputed(raw) {
		return "DISPUTED"
	}

	// Check for high volume (HOT)
	totalVolume := 0.0
	for _, m := range raw.Markets {
		totalVolume += m.Volume24hr.Float64()
	}
	if totalVolume > 500000 {
		return "HOT"
	}

	return ""
}

// =============================================================================
// Step 7: Build Ticker
// =============================================================================

// buildTicker generates a short ticker symbol for the event.
func buildTicker(raw RawGammaEvent, markets []RawGammaMarket, layout string) string {
	switch layout {
	case LayoutPoll:
		// Use first market's GroupItemTitle, uppercase
		if len(markets) > 0 && markets[0].GroupItemTitle != "" {
			return strings.ToUpper(markets[0].GroupItemTitle)
		}
		return strings.ToUpper(truncate(raw.Slug, 10))

	case LayoutSports:
		// "TeamA vs TeamB" format
		if len(markets) > 0 {
			outcomes := parseRawOutcomes(markets[0].OutcomesRaw)
			if len(outcomes) >= 2 {
				return fmt.Sprintf("%s vs %s", outcomes[0], outcomes[1])
			}
		}
		return strings.ToUpper(truncate(raw.Slug, 10))

	default:
		// BINARY: use slug or truncated title
		if raw.Slug != "" {
			return strings.ToUpper(truncate(raw.Slug, 10))
		}
		return strings.ToUpper(truncate(raw.Title, 10))
	}
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
