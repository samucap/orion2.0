package handlers

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/samucap/orion2.0/internal/db"
)

// TransformEventV2 converts a raw Gamma API event into a V2 event with fixed classification.
// Returns nil if the event has no markets.
func TransformEventV2(raw RawGammaEvent, teamsByName map[string]db.PlyMktTeam, leaguesBySlug map[string]db.League) *V2Event {
	// Skip events with no markets
	if len(raw.Markets) == 0 {
		return nil
	}

	// Classify event type (sports first, then market count)
	// negRisk event?
	//
	var v2Event V2Event
	if IsSportsEvent(raw) {
		if isSportsGroupEvent(raw) {
			v2Event = buildSportsGroupEvent(raw, teamsByName, leaguesBySlug)
		} else {
			v2Event = buildSportsEvent(raw, teamsByName, leaguesBySlug)
		}
	} else if len(raw.Markets) > 1 {
		v2Event = buildGroupEvent(raw)
	} else {
		v2Event = buildBinaryEvent(raw)
	}

	return &v2Event
}

// IsSportsEvent checks if an event should be classified as sports.
func IsSportsEvent(raw RawGammaEvent) bool {
	for _, tag := range raw.Tags {
		if tag.Slug == "sports" || tag.Slug == "esports" {
			return true
		}
	}

	return raw.GameID.String() != "" || raw.Category == "Sports"
}

// isSportsGroupEvent checks if an event should be classified as sports_group (futures/champions).
// These are sports-tagged events without a specific game ID but with multiple markets.
func isSportsGroupEvent(raw RawGammaEvent) bool {
	// Futures/champions: sports-tagged, no specific game, multiple markets
	return IsSportsEvent(raw) && raw.GameID.String() == "" && len(raw.Markets) > 1
}

// buildSportsEvent creates a V2Event for sports events.
func buildSportsEvent(raw RawGammaEvent, teamsByName map[string]db.PlyMktTeam, leaguesBySlug map[string]db.League) V2Event {
	// Use enrichment logic to build displayData
	displayData := EnrichSportsEvent(raw, teamsByName, leaguesBySlug)

	// Calculate stats, status
	stats := calculateV2Stats(raw.Markets)
	statusBadge := buildV2StatusBadge(raw, stats)

	// Populate outcomes with market data for trading
	outcomes := buildSportsOutcomes(raw, teamsByName, leaguesBySlug)

	// Determine image (keep original Polymarket image, enriched data is in DisplayData)
	image := raw.Icon
	if image == "" {
		image = raw.Image
	}

	// Set original image for fallback
	originalImage := raw.Icon
	if originalImage == "" {
		originalImage = raw.Image
	}

	return V2Event{
		ID:            raw.ID,
		Title:         raw.Title,
		Subtitle:      raw.Description,
		Image:         image,
		OriginalImage: originalImage,
		TotalVolume:   raw.Volume.Float64(),
		EndDate:       raw.EndDate,
		Liquidity:     raw.Liquidity.Float64(),
		Volume24hr:    raw.Volume24hr.Float64(),
		DisplayType:   "sports",
		IsLive:        raw.Live, // Use actual live status instead of active
		StatusBadge:   statusBadge,
		Stats:         stats,
		Outcomes:      outcomes,    // Include market data for trading
		DisplayData:   displayData, // Include enriched display data
		Score:         raw.Score,
		Period:        raw.Period,
		StartTime:     raw.StartTime,
	}
}

// buildSportsGroupEvent creates a V2Event for sports futures/champions events.
// These events have multiple markets, each representing a team outcome.
func buildSportsGroupEvent(raw RawGammaEvent, teamsByName map[string]db.PlyMktTeam, leaguesBySlug map[string]db.League) V2Event {
	// Use enrichment logic to build displayData
	displayData := EnrichSportsEvent(raw, teamsByName, leaguesBySlug)

	// Keep the original outcomes logic for group events (tournament winners)
	outcomes := make([]V2Outcome, 0)

	// Collect all markets with their "Yes" prices for sorting
	type marketWithPrice struct {
		market RawGammaMarket
		price  float64
	}

	marketPrices := make([]marketWithPrice, 0, len(raw.Markets))
	for _, market := range raw.Markets {
		prices := parseRawPrices(market.OutcomePricesRaw)
		price := 0.0
		if len(prices) > 0 {
			price = prices[0] // "Yes" price
		}

		marketPrices = append(marketPrices, marketWithPrice{
			market: market,
			price:  price,
		})
	}

	// Sort by price DESC (highest probability first)
	sort.Slice(marketPrices, func(i, j int) bool {
		return marketPrices[i].price > marketPrices[j].price
	})

	eventLeague := EventLeagueSlug(raw, leaguesBySlug)
	evImg := eventImage(raw)

	for _, mp := range marketPrices {
		label := mp.market.GroupItemTitle
		if label == "" {
			label = stripEventTitle(mp.market.Question, raw.Title)
		}

		team := TeamByLabel(teamsByName, eventLeague, label, raw)
		outcomes = append(outcomes, V2Outcome{
			MarketID:           mp.market.ID,
			Label:              label,
			Probability:        mp.price,
			Price:              mp.price,
			BestBid:            mp.market.BestBid.Float64(),
			BestAsk:            mp.market.BestAsk.Float64(),
			Liquidity:          mp.market.Liquidity.Float64(),
			Volume24hr:         mp.market.Volume24hr.Float64(),
			Status:             "active",
			Color:              ResolveTeamColor(team),
			Image:              ResolveTeamImage(label, team, raw, eventLeague, leaguesBySlug, mp.market.Image, evImg, teamsByName),
			SportsMarketType:   mp.market.SportsMarketType,
			EndDate:            mp.market.EndDate,
			GameStartTime:      mp.market.GameStartTime,
			Volume24hrClob:     mp.market.Volume24hrClob.Float64(),
			LiquidityClob:      mp.market.LiquidityClob.Float64(),
			OneDayPriceChange:  mp.market.OneDayPriceChange.Float64(),
			OneHourPriceChange: mp.market.OneHourPriceChange.Float64(),
			ClobTokenIds:       mp.market.ClobTokenIds,
		})
	}

	// Determine image (keep original Polymarket image, enriched data is in DisplayData)
	image := raw.Icon
	if image == "" {
		image = raw.Image
	}

	// Set original image for fallback
	originalImage := raw.Icon
	if originalImage == "" {
		originalImage = raw.Image
	}

	// Calculate stats, status
	stats := calculateV2Stats(raw.Markets)
	statusBadge := buildV2StatusBadge(raw, stats)

	return V2Event{
		ID:            raw.ID,
		Title:         raw.Title,
		Subtitle:      raw.Description,
		Image:         image,
		OriginalImage: originalImage,
		TotalVolume:   raw.Volume.Float64(),
		EndDate:       raw.EndDate,
		Liquidity:     raw.Liquidity.Float64(),
		Volume24hr:    raw.Volume24hr.Float64(),
		DisplayType:   "sports_group",
		IsLive:        raw.Live,
		StatusBadge:   statusBadge,
		Stats:         stats,
		Outcomes:      outcomes,
		DisplayData:   displayData,
		Score:         raw.Score,
		Period:        raw.Period,
		StartTime:     raw.StartTime,
	}
}

// buildGroupEvent creates a V2Event for group events (multiple markets).
func buildGroupEvent(raw RawGammaEvent) V2Event {
	outcomes := make([]V2Outcome, 0)

	// Collect all markets with their "Yes" prices for sorting
	type marketWithPrice struct {
		market RawGammaMarket
		price  float64
		status string
	}

	marketPrices := make([]marketWithPrice, 0, len(raw.Markets))
	for _, market := range raw.Markets {
		prices := parseRawPrices(market.OutcomePricesRaw)
		price := 0.0
		if len(prices) > 0 {
			price = prices[0] // "Yes" price
		}

		// Determine status
		status := "active"
		if market.Closed {
			status = "resolved"
		}

		marketPrices = append(marketPrices, marketWithPrice{
			market: market,
			price:  price,
			status: status,
		})
	}

	// Sort by price DESC (highest probability first)
	sort.Slice(marketPrices, func(i, j int) bool {
		return marketPrices[i].price > marketPrices[j].price
	})

	// Take top 5, skip outcomes with zero probability
	for _, mp := range marketPrices {
		label := mp.market.GroupItemTitle
		if label == "" {
			// Fallback: strip event title from question
			label = stripEventTitle(mp.market.Question, raw.Title)
		}

		outcomes = append(outcomes, V2Outcome{
			MarketID:           mp.market.ID,
			Label:              label,
			Probability:        mp.price,
			Status:             mp.status,
			Color:              "",
			Image:              mp.market.Image,
			EndDate:            mp.market.EndDate,
			GameStartTime:      mp.market.GameStartTime,
			Volume24hrClob:     mp.market.Volume24hrClob.Float64(),
			LiquidityClob:      mp.market.LiquidityClob.Float64(),
			OneDayPriceChange:  mp.market.OneDayPriceChange.Float64(),
			OneHourPriceChange: mp.market.OneHourPriceChange.Float64(),
			ClobTokenIds:       mp.market.ClobTokenIds,
		})
	}

	// Determine image (icon preferred, fallback to image)
	image := raw.Icon
	if image == "" {
		image = raw.Image
	}

	// Calculate stats and status
	stats := calculateV2Stats(raw.Markets)
	statusBadge := buildV2StatusBadge(raw, stats)

	return V2Event{
		ID:            raw.ID,
		Title:         raw.Title,
		Subtitle:      raw.Description,
		Image:         image,
		OriginalImage: image, // Group events don't have enriched images
		TotalVolume:   raw.Volume.Float64(),
		EndDate:       raw.EndDate,
		Liquidity:     raw.Liquidity.Float64(),
		Volume24hr:    raw.Volume24hr.Float64(),
		DisplayType:   "group",
		IsLive:        raw.Live,
		StatusBadge:   statusBadge,
		Stats:         stats,
		Outcomes:      outcomes,
		Score:         raw.Score,
		Period:        raw.Period,
		StartTime:     raw.StartTime,
	}
}

// buildBinaryEvent creates a V2Event for binary events (single market).
func buildBinaryEvent(raw RawGammaEvent) V2Event {
	market := raw.Markets[0]
	prices := parseRawPrices(market.OutcomePricesRaw)
	outcomeLabels := parseRawOutcomes(market.OutcomesRaw)

	// Default labels if not available
	if len(outcomeLabels) == 0 {
		outcomeLabels = []string{"Yes", "No"}
	}

	// Determine status
	status := "active"
	if market.Closed {
		status = "resolved"
	}

	outcomes := make([]V2Outcome, 0, 2)
	for i, label := range outcomeLabels {
		if i >= 2 {
			break // Only Yes/No for binary
		}

		price := 0.0
		if i < len(prices) {
			price = prices[i]
		}

		outcomes = append(outcomes, V2Outcome{
			MarketID:           market.ID,
			Label:              label,
			Probability:        price,
			Price:              price,
			BestBid:            market.BestBid.Float64(),
			BestAsk:            market.BestAsk.Float64(),
			Liquidity:          market.Liquidity.Float64(),
			Volume24hr:         market.Volume24hr.Float64(),
			Status:             status,
			Color:              "",
			Image:              market.Image,
			EndDate:            market.EndDate,
			GameStartTime:      market.GameStartTime,
			Volume24hrClob:     market.Volume24hrClob.Float64(),
			LiquidityClob:      market.LiquidityClob.Float64(),
			OneDayPriceChange:  market.OneDayPriceChange.Float64(),
			OneHourPriceChange: market.OneHourPriceChange.Float64(),
			ClobTokenIds:       market.ClobTokenIds,
		})
	}

	// Determine image (icon preferred, fallback to image)
	image := raw.Icon
	if image == "" {
		image = raw.Image
	}

	// Calculate stats and status
	stats := calculateV2Stats(raw.Markets)
	statusBadge := buildV2StatusBadge(raw, stats)

	return V2Event{
		ID:            raw.ID,
		Title:         raw.Title,
		Subtitle:      raw.Description,
		Image:         image,
		OriginalImage: image, // Binary events don't have enriched images
		TotalVolume:   raw.Volume.Float64(),
		EndDate:       raw.EndDate,
		Liquidity:     raw.Liquidity.Float64(),
		Volume24hr:    raw.Volume24hr.Float64(),
		DisplayType:   "binary",
		IsLive:        raw.Live,
		StatusBadge:   statusBadge,
		Stats:         stats,
		Outcomes:      outcomes,
		Score:         raw.Score,
		Period:        raw.Period,
		StartTime:     raw.StartTime,
	}
}

// calculateV2Stats computes trading statistics for V2 events
func calculateV2Stats(markets []RawGammaMarket) V2EventStats {
	if len(markets) == 0 {
		return V2EventStats{}
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

	// Determine whale action signal (high volume + tight spread)
	isWhaleAction := totalVolume > 100000 && spreadBP < 200

	return V2EventStats{
		VolumeUSD:     formatV2VolumeUSD(totalVolume),
		SpreadBP:      spreadBP,
		IsWhaleAction: isWhaleAction,
	}
}

// buildV2StatusBadge determines status badge for V2 events
func buildV2StatusBadge(raw RawGammaEvent, stats V2EventStats) string {
	// Check for disputed status first
	if isV2EventDisputed(raw) {
		var currStatus string
		if raw.UmaResolutionStatus != "" {
			currStatus = raw.UmaResolutionStatus
		} else if raw.UmaResolutionStatuses != "" {
			statuses := strings.Split(raw.UmaResolutionStatuses, "\\")
			currStatus = statuses[len(statuses)-1]
		}
		return fmt.Sprintf("DISPUTED %s", currStatus)
	}

	return ""
}

// isV2EventDisputed checks if event has disputed status
func isV2EventDisputed(raw RawGammaEvent) bool {
	statuses := strings.ToLower(raw.UmaResolutionStatuses)
	return strings.Contains(statuses, "disputed") || strings.Contains(statuses, "answering") || strings.Contains(statuses, "pending")
}

// buildSportsOutcomes creates V2Outcome array for sports events with market trading data
func buildSportsOutcomes(raw RawGammaEvent, teamsByName map[string]db.PlyMktTeam, leaguesBySlug map[string]db.League) []V2Outcome {
	// Find the primary market (moneyline preferred, then highest liquidity)
	mainMarket := primarySportsMarket(raw)

	if mainMarket == nil {
		return nil
	}

	// Parse outcomes and prices from the main market
	outcomes := parseRawOutcomes(mainMarket.OutcomesRaw)
	prices := parseRawPrices(mainMarket.OutcomePricesRaw)

	if len(outcomes) == 0 {
		return nil
	}

	eventLeague := EventLeagueSlug(raw, leaguesBySlug)
	evImg := eventImage(raw)

	// Soccer moneyline detection: if outcomes are Yes/No, build one outcome
	// per market using GroupItemTitle as the label and the "Yes" price.
	if isYesNoOutcomes(outcomes) && len(raw.Markets) > 1 {
		return buildMoneylineOutcomes(raw, teamsByName, leaguesBySlug, eventLeague, evImg)
	}

	v2Outcomes := make([]V2Outcome, 0, len(outcomes))

	for i, label := range outcomes {
		price := 0.0
		if i < len(prices) {
			price = prices[i]
		}
		team := TeamByLabel(teamsByName, eventLeague, label, raw)
		status := "active"
		if mainMarket.Closed {
			status = "resolved"
		}
		v2Outcomes = append(v2Outcomes, V2Outcome{
			MarketID:           mainMarket.ID,
			Label:              label,
			Probability:        price,
			Price:              price,
			BestBid:            mainMarket.BestBid.Float64(),
			BestAsk:            mainMarket.BestAsk.Float64(),
			Liquidity:          mainMarket.Liquidity.Float64(),
			Volume24hr:         mainMarket.Volume24hr.Float64(),
			Status:             status,
			Color:              ResolveTeamColor(team),
			Image:              ResolveTeamImage(label, team, raw, eventLeague, leaguesBySlug, mainMarket.Image, evImg, teamsByName),
			EndDate:            mainMarket.EndDate,
			GameStartTime:      mainMarket.GameStartTime,
			Volume24hrClob:     mainMarket.Volume24hrClob.Float64(),
			LiquidityClob:      mainMarket.LiquidityClob.Float64(),
			OneDayPriceChange:  mainMarket.OneDayPriceChange.Float64(),
			OneHourPriceChange: mainMarket.OneHourPriceChange.Float64(),
			ClobTokenIds:       mainMarket.ClobTokenIds,
		})
	}

	return v2Outcomes
}

// buildMoneylineOutcomes creates outcomes from per-team Yes/No markets (soccer moneyline style).
// Each market's GroupItemTitle becomes the label, "Yes" price becomes probability.
func buildMoneylineOutcomes(raw RawGammaEvent, teamsByName map[string]db.PlyMktTeam, leaguesBySlug map[string]db.League, eventLeague, evImg string) []V2Outcome {
	v2Outcomes := make([]V2Outcome, 0, len(raw.Markets))
	for _, m := range raw.Markets {
		label := strings.TrimSpace(m.GroupItemTitle)
		if label == "" {
			continue
		}
		prices := parseRawPrices(m.OutcomePricesRaw)
		yesPrice := 0.0
		if len(prices) > 0 {
			yesPrice = prices[0]
		}
		team := TeamByLabel(teamsByName, eventLeague, label, raw)
		status := "active"
		if m.Closed {
			status = "resolved"
		}
		v2Outcomes = append(v2Outcomes, V2Outcome{
			MarketID:           m.ID,
			Label:              label,
			Probability:        yesPrice,
			Price:              yesPrice,
			BestBid:            m.BestBid.Float64(),
			BestAsk:            m.BestAsk.Float64(),
			Liquidity:          m.Liquidity.Float64(),
			Volume24hr:         m.Volume24hr.Float64(),
			Status:             status,
			Color:              ResolveTeamColor(team),
			Image:              ResolveTeamImage(label, team, raw, eventLeague, leaguesBySlug, m.Image, evImg, teamsByName),
			SportsMarketType:   m.SportsMarketType,
			EndDate:            m.EndDate,
			GameStartTime:      m.GameStartTime,
			Volume24hrClob:     m.Volume24hrClob.Float64(),
			LiquidityClob:      m.LiquidityClob.Float64(),
			OneDayPriceChange:  m.OneDayPriceChange.Float64(),
			OneHourPriceChange: m.OneHourPriceChange.Float64(),
			ClobTokenIds:       m.ClobTokenIds,
		})
	}
	return v2Outcomes
}

// formatV2VolumeUSD formats volume as human-readable string
func formatV2VolumeUSD(volume float64) string {
	switch {
	case volume >= 1000000:
		return fmt.Sprintf("$%.1fM", volume/1000000)
	case volume >= 1000:
		return fmt.Sprintf("$%.0fK", volume/1000)
	default:
		return fmt.Sprintf("$%.0f", volume)
	}
}

// stripEventTitle extracts the candidate name from a question by stripping the event title.
// Examples:
//
//	"Will Donald Trump win the 2024 US Presidential Election?" -> "Donald Trump"
//	"Will Kamala Harris win 2024 Presidential Election?" -> "Kamala Harris"
func stripEventTitle(question, eventTitle string) string {
	if question == "" {
		return ""
	}
	if eventTitle == "" {
		return question
	}

	// Clean up the question and event title for regex matching
	cleanQuestion := strings.TrimSpace(question)
	cleanEventTitle := strings.TrimSpace(eventTitle)

	// Escape special regex characters in event title
	escapedTitle := regexp.QuoteMeta(cleanEventTitle)

	// Patterns to try (most specific to least specific)
	patterns := []string{
		`(?i)^Will\s+(.+?)\s+win\s+(?:the\s+)?` + escapedTitle + `\??$`,
		`(?i)^Will\s+(.+?)\s+win` + escapedTitle + `\??$`,
		`(?i)^(.+?)\s+` + escapedTitle + `\??$`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(cleanQuestion)
		if len(matches) > 1 {
			candidate := strings.TrimSpace(matches[1])
			if candidate != "" {
				return candidate
			}
		}
	}

	// If no pattern matches, return the original question
	return cleanQuestion
}

// primarySportsMarket returns the preferred market for sports events.
// Prioritizes moneyline markets, falls back to highest liquidity market.
func primarySportsMarket(raw RawGammaEvent) *RawGammaMarket {
	for i := range raw.Markets {
		if raw.Markets[i].SportsMarketType == "moneyline" {
			return &raw.Markets[i]
		}
	}
	return highestLiquidityMarket(raw)
}

// sortMarketsMoneylineFirst sorts markets so moneyline markets appear first.
// This ensures Outcomes[0] comes from the moneyline market when available.
func sortMarketsMoneylineFirst(markets []RawGammaMarket) {
	sort.SliceStable(markets, func(i, j int) bool {
		return markets[i].SportsMarketType == "moneyline" && markets[j].SportsMarketType != "moneyline"
	})
}
