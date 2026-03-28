package handlers_test

import (
	"testing"

	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestGroupEventClassification(t *testing.T) {
	// Event with 5 markets (4 resolved, 1 active) should be classified as "group"
	rawEvent := handlers.RawGammaEvent{
		ID:     "group-event-123",
		Title:  "2024 US Presidential Election",
		Icon:   "election-icon.png",
		Volume: handlers.FlexFloat(1000000),
		Markets: []handlers.RawGammaMarket{
			{ID: "market-1", GroupItemTitle: "Biden", Active: false, Closed: true, OutcomePricesRaw: `["0.05"]`},
			{ID: "market-2", GroupItemTitle: "Trump", Active: true, Closed: false, OutcomePricesRaw: `["0.65"]`},
			{ID: "market-3", GroupItemTitle: "Harris", Active: false, Closed: true, OutcomePricesRaw: `["0.15"]`},
			{ID: "market-4", GroupItemTitle: "Haley", Active: false, Closed: true, OutcomePricesRaw: `["0.10"]`},
			{ID: "market-5", GroupItemTitle: "DeSantis", Active: false, Closed: true, OutcomePricesRaw: `["0.05"]`},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "group-event-123", result.ID)
	assert.Equal(t, "2024 US Presidential Election", result.Title)
	assert.Equal(t, "election-icon.png", result.Image)
	assert.Equal(t, 1000000.0, result.TotalVolume)
	assert.Equal(t, "group", result.DisplayType)

	// Should have 5 outcomes (all have non-zero probabilities)
	assert.Len(t, result.Outcomes, 5)
	assert.Equal(t, "Trump", result.Outcomes[0].Label)
	assert.Equal(t, 0.65, result.Outcomes[0].Probability)
	assert.Equal(t, "active", result.Outcomes[0].Status)

	assert.Equal(t, "Harris", result.Outcomes[1].Label)
	assert.Equal(t, 0.15, result.Outcomes[1].Probability)
	assert.Equal(t, "resolved", result.Outcomes[1].Status)
}

func TestBinaryEventClassification(t *testing.T) {
	// Event with 1 market should be classified as "binary"
	rawEvent := handlers.RawGammaEvent{
		ID:     "binary-event-456",
		Title:  "Will it rain tomorrow?",
		Image:  "weather-icon.png",
		Volume: handlers.FlexFloat(50000),
		Markets: []handlers.RawGammaMarket{
			{ID: "market-1", Active: true, Closed: false, OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.30", "0.70"]`},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "binary-event-456", result.ID)
	assert.Equal(t, "Will it rain tomorrow?", result.Title)
	assert.Equal(t, "weather-icon.png", result.Image)
	assert.Equal(t, 50000.0, result.TotalVolume)
	assert.Equal(t, "binary", result.DisplayType)

	// Should have Yes/No outcomes
	assert.Len(t, result.Outcomes, 2)
	assert.Equal(t, "Yes", result.Outcomes[0].Label)
	assert.Equal(t, 0.30, result.Outcomes[0].Probability)
	assert.Equal(t, "active", result.Outcomes[0].Status)

	assert.Equal(t, "No", result.Outcomes[1].Label)
	assert.Equal(t, 0.70, result.Outcomes[1].Probability)
	assert.Equal(t, "active", result.Outcomes[1].Status)
}

func TestGroupOutcomeSorting(t *testing.T) {
	// Test that outcomes are sorted by probability descending and limited to top 5
	rawEvent := handlers.RawGammaEvent{
		ID:     "sort-test",
		Title:  "Test Event",
		Volume: handlers.FlexFloat(100000),
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "First", Active: true, Closed: false, OutcomePricesRaw: `["0.10"]`},  // 5th
			{ID: "m2", GroupItemTitle: "Second", Active: true, Closed: false, OutcomePricesRaw: `["0.90"]`}, // 1st
			{ID: "m3", GroupItemTitle: "Third", Active: true, Closed: false, OutcomePricesRaw: `["0.20"]`},  // 4th
			{ID: "m4", GroupItemTitle: "Fourth", Active: true, Closed: false, OutcomePricesRaw: `["0.70"]`}, // 2nd
			{ID: "m5", GroupItemTitle: "Fifth", Active: true, Closed: false, OutcomePricesRaw: `["0.50"]`},  // 3rd
			{ID: "m6", GroupItemTitle: "Sixth", Active: true, Closed: false, OutcomePricesRaw: `["0.05"]`},  // 6th (excluded)
			{ID: "m7", GroupItemTitle: "Zero", Active: true, Closed: false, OutcomePricesRaw: `["0.00"]`},   // excluded
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Len(t, result.Outcomes, 7)

	// Check sorting by probability descending
	assert.Equal(t, "Second", result.Outcomes[0].Label)
	assert.Equal(t, 0.90, result.Outcomes[0].Probability)

	assert.Equal(t, "Fourth", result.Outcomes[1].Label)
	assert.Equal(t, 0.70, result.Outcomes[1].Probability)

	assert.Equal(t, "Fifth", result.Outcomes[2].Label)
	assert.Equal(t, 0.50, result.Outcomes[2].Probability)

	assert.Equal(t, "Third", result.Outcomes[3].Label)
	assert.Equal(t, 0.20, result.Outcomes[3].Probability)

	assert.Equal(t, "First", result.Outcomes[4].Label)
	assert.Equal(t, 0.10, result.Outcomes[4].Probability)
}

func TestGroupLabelFallback(t *testing.T) {
	// Test regex fallback when GroupItemTitle is empty
	rawEvent := handlers.RawGammaEvent{
		ID:     "fallback-test",
		Title:  "2024 Election",
		Volume: handlers.FlexFloat(100000),
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", Question: "Will Joe Biden win the 2024 Election?", Active: true, Closed: false, OutcomePricesRaw: `["0.40"]`},
			{ID: "m2", Question: "Will Donald Trump win 2024 Election?", Active: true, Closed: false, OutcomePricesRaw: `["0.60"]`},
			{ID: "m3", GroupItemTitle: "Kamala Harris", Question: "Will Kamala Harris win the 2024 Election?", Active: true, Closed: false, OutcomePricesRaw: `["0.20"]`},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Len(t, result.Outcomes, 3)

	// Check fallback extraction worked
	assert.Equal(t, "Donald Trump", result.Outcomes[0].Label) // Highest probability first
	assert.Equal(t, "Joe Biden", result.Outcomes[1].Label)
	assert.Equal(t, "Kamala Harris", result.Outcomes[2].Label) // Had GroupItemTitle, no fallback needed
}

func TestTotalVolumePassthrough(t *testing.T) {
	// Test that totalVolume comes directly from event.Volume field
	rawEvent := handlers.RawGammaEvent{
		ID:     "volume-test",
		Title:  "Volume Test",
		Volume: handlers.FlexFloat(1234567.89),
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "Option A", Active: true, Closed: false, OutcomePricesRaw: `["0.50"]`},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, 1234567.89, result.TotalVolume)
}

func TestEmptyMarketsSkipped(t *testing.T) {
	// Event with no markets should return nil
	rawEvent := handlers.RawGammaEvent{
		ID:      "empty-test",
		Title:   "Empty Event",
		Markets: []handlers.RawGammaMarket{},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.Nil(t, result)
}

func TestSportsEventClassification(t *testing.T) {
	// Event with Category == "Sports" should be classified as "sports"
	rawEvent := handlers.RawGammaEvent{
		ID:       "sports-test",
		Title:    "Los Angeles Lakers vs Boston Celtics",
		Category: "Sports",
		Icon:     "nba-icon.png",
		Volume:   handlers.FlexFloat(500000),
		Tags:     []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "nba"}},
		Teams: []handlers.RawGammaTeam{
			{Name: "Los Angeles Lakers", Abbreviation: "LAL", Logo: "lakers-logo.png"},
			{Name: "Boston Celtics", Abbreviation: "BOS", Logo: "celtics-logo.png"},
		},
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-1",
				SportsMarketType: "basketball",
				TeamAID:          "LAL",
				TeamBID:          "BOS",
				OutcomesRaw:      `["Los Angeles Lakers", "Boston Celtics"]`,
				OutcomePricesRaw: `["0.55", "0.45"]`,
				Liquidity:        handlers.FlexFloat(100000),
				Active:           true,
				Closed:           false,
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}
	teamsByName := map[string]db.PlyMktTeam{
		"los angeles lakers": {Name: "Los Angeles Lakers", Logo: "lakers-logo.png"},
		"boston celtics":     {Name: "Boston Celtics", Logo: "celtics-logo.png"},
	}
	result := handlers.TransformEventV2(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "sports-test", result.ID)
	assert.Equal(t, "Los Angeles Lakers vs Boston Celtics", result.Title)
	assert.Equal(t, "nba-icon.png", result.Image)
	assert.Equal(t, 500000.0, result.TotalVolume)
	assert.Equal(t, "sports", result.DisplayType)

	// Should have both DisplayData and Outcomes
	assert.NotNil(t, result.DisplayData)
	assert.NotNil(t, result.Outcomes)
	assert.Len(t, result.Outcomes, 2) // Two market outcomes
	assert.Equal(t, "versus_match", result.DisplayData.Type)
	assert.Len(t, result.DisplayData.Participants, 2)

	// Check outcomes (market data)
	assert.Equal(t, "Los Angeles Lakers", result.Outcomes[0].Label)
	assert.Equal(t, 0.55, result.Outcomes[0].Probability)
	assert.Equal(t, "lakers-logo.png", result.Outcomes[0].Image)
	assert.Equal(t, "Boston Celtics", result.Outcomes[1].Label)
	assert.Equal(t, 0.45, result.Outcomes[1].Probability)
	assert.Equal(t, "celtics-logo.png", result.Outcomes[1].Image)

	// Check Home team (Lakers) - first participant
	home := result.DisplayData.Participants[0]
	assert.Equal(t, "Los Angeles Lakers", home.Name)
	assert.Equal(t, "home", home.Role)
	assert.Equal(t, "lakers-logo.png", home.ImageURL)

	// Check Away team (Celtics) - second participant
	away := result.DisplayData.Participants[1]
	assert.Equal(t, "Boston Celtics", away.Name)
	assert.Equal(t, "away", away.Role)
	assert.Equal(t, "celtics-logo.png", away.ImageURL)
}

func TestSportsGroupEventClassification(t *testing.T) {
	// Event for NBA Champions futures: sports tag, no gameId, multiple markets
	rawEvent := handlers.RawGammaEvent{
		ID:       "nba-champions-2025",
		Title:    "NBA Champions 2024-25",
		Category: "Sports",
		NegRisk:  true,
		// No GameID - this is a futures event
		Volume: handlers.FlexFloat(500000),
		Tags: []handlers.RawGammaTag{
			{Slug: "sports"},
		},
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "Boston Celtics", OutcomePricesRaw: `["0.15"]`, SportsMarketType: "futures"},
			{ID: "m2", GroupItemTitle: "Milwaukee Bucks", OutcomePricesRaw: `["0.12"]`, SportsMarketType: "futures"},
			{ID: "m3", GroupItemTitle: "Denver Nuggets", OutcomePricesRaw: `["0.20"]`, SportsMarketType: "futures"},
			{ID: "m4", GroupItemTitle: "Phoenix Suns", OutcomePricesRaw: `["0.10"]`, SportsMarketType: "futures"},
			{ID: "m5", GroupItemTitle: "Golden State Warriors", OutcomePricesRaw: `["0.08"]`, SportsMarketType: "futures"},
			{ID: "m6", GroupItemTitle: "Los Angeles Lakers", OutcomePricesRaw: `["0.05"]`, SportsMarketType: "futures"}, // Zero probability, should be excluded
		},
	}

	// Mock teams data
	teamsByName := map[string]db.PlyMktTeam{
		"boston celtics":        {Name: "Boston Celtics", Logo: "celtics-logo.png", Color: "#007A33"},
		"milwaukee bucks":       {Name: "Milwaukee Bucks", Logo: "bucks-logo.png", Color: "#00471B"},
		"denver nuggets":        {Name: "Denver Nuggets", Logo: "nuggets-logo.png", Color: "#0E2240"},
		"phoenix suns":          {Name: "Phoenix Suns", Logo: "suns-logo.png", Color: "#1D1160"},
		"golden state warriors": {Name: "Golden State Warriors", Logo: "warriors-logo.png", Color: "#1D428A"},
	}

	result := handlers.TransformEventV2(rawEvent, teamsByName, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "nba-champions-2025", result.ID)
	assert.Equal(t, "NBA Champions 2024-25", result.Title)
	assert.Equal(t, 500000.0, result.TotalVolume)
	assert.Equal(t, "sports_group", result.DisplayType)

	// Should have 6 outcomes (top ranking, all markets included)
	assert.Len(t, result.Outcomes, 6)
	assert.Equal(t, "Denver Nuggets", result.Outcomes[0].Label) // Highest probability first
	assert.Equal(t, 0.20, result.Outcomes[0].Probability)
	assert.Equal(t, "active", result.Outcomes[0].Status)
	assert.Equal(t, "nuggets-logo.png", result.Outcomes[0].Image)
	assert.Equal(t, "#0E2240", result.Outcomes[0].Color)
	assert.Equal(t, "futures", result.Outcomes[0].SportsMarketType)

	assert.Equal(t, "Boston Celtics", result.Outcomes[1].Label)
	assert.Equal(t, 0.15, result.Outcomes[1].Probability)
	assert.Equal(t, "celtics-logo.png", result.Outcomes[1].Image)
	assert.Equal(t, "#007A33", result.Outcomes[1].Color)
}

func TestSportsEventDBFallback(t *testing.T) {
	// Sports event without raw.Teams data (eSports scenario)
	rawEvent := handlers.RawGammaEvent{
		ID:       "cs2-match-123",
		Title:    "Team A vs Team B",
		Category: "Sports",
		GameID:   "hltv123", // Has GameID so it's not sports_group
		Volume:   handlers.FlexFloat(10000),
		Tags: []handlers.RawGammaTag{
			{Slug: "sports"},
			{Slug: "cs2"},
		},
		// No raw.Teams data - should fallback to DB
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-1",
				SportsMarketType: "moneyline",
				TeamAID:          "TA",
				TeamBID:          "TB",
				OutcomesRaw:      `["Team A", "Team B"]`,
				OutcomePricesRaw: `["0.55", "0.45"]`,
				Liquidity:        handlers.FlexFloat(5000),
				Active:           true,
				Closed:           false,
			},
		},
	}

	// Mock league and team data
	leaguesBySlug := map[string]db.League{
		"cs2": {Sport: "cs2", Ordering: "home"},
	}
	teamsByName := map[string]db.PlyMktTeam{
		"team a": {Name: "Team A", Logo: "team-a-logo.png", Color: "#FF0000"},
		"team b": {Name: "Team B", Logo: "team-b-logo.png", Color: "#0000FF"},
	}

	result := handlers.TransformEventV2(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "cs2-match-123", result.ID)
	assert.Equal(t, "sports", result.DisplayType)

	// Should have both DisplayData and Outcomes
	assert.NotNil(t, result.DisplayData)
	assert.NotNil(t, result.Outcomes)
	assert.Len(t, result.Outcomes, 2)
	assert.Equal(t, "versus_match", result.DisplayData.Type)
	assert.Len(t, result.DisplayData.Participants, 2)

	// Check Home team (Team A) - first participant
	home := result.DisplayData.Participants[0]
	assert.Equal(t, "Team A", home.Name)
	assert.Equal(t, "home", home.Role)
	assert.Equal(t, "team-a-logo.png", home.ImageURL)

	// Check Away team (Team B) - second participant
	away := result.DisplayData.Participants[1]
	assert.Equal(t, "Team B", away.Name)
	assert.Equal(t, "away", away.Role)
	assert.Equal(t, "team-b-logo.png", away.ImageURL)
}

func TestSportsVersusMarketImageFallback(t *testing.T) {
	// When DB and raw team lookup fail, outcomes and DisplayData participants fall back to market image
	rawEvent := handlers.RawGammaEvent{
		ID:       "nhl-match",
		Title:    "Team X vs Team Y",
		Category: "Sports",
		Volume:   handlers.FlexFloat(5000),
		Tags:     []handlers.RawGammaTag{{Slug: "sports"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-1",
				OutcomesRaw:      `["Team X", "Team Y"]`,
				OutcomePricesRaw: `["0.50", "0.50"]`,
				Image:            "nhl-matchup.png",
				Liquidity:        handlers.FlexFloat(1000),
				Active:           true,
				Closed:           false,
			},
		},
	}

	teamsByName := map[string]db.PlyMktTeam{}
	leaguesBySlug := map[string]db.League{}

	result := handlers.TransformEventV2(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "sports", result.DisplayType)
	assert.Len(t, result.Outcomes, 2)
	assert.Equal(t, "nhl-matchup.png", result.Outcomes[0].Image)
	assert.Equal(t, "nhl-matchup.png", result.Outcomes[1].Image)
	assert.NotNil(t, result.DisplayData)
	assert.Len(t, result.DisplayData.Participants, 2)
	assert.Equal(t, "nhl-matchup.png", result.DisplayData.Participants[0].ImageURL)
	assert.Equal(t, "nhl-matchup.png", result.DisplayData.Participants[1].ImageURL)
}

// =============================================================================
// IMAGE ACCURACY TESTS (CRITICAL - LOCKS IN PER-MARKET IMAGE BEHAVIOR)
// =============================================================================

func TestGroupEventPreservesPerMarketImages(t *testing.T) {
	// Politics event with 3 markets each having a unique candidate image.
	// Each outcome's .Image should match its own market's image, not the event image or any other market's image.
	rawEvent := handlers.RawGammaEvent{
		ID:     "politics-election-2024",
		Title:  "2024 US Presidential Election Winner",
		Icon:   "election-generic.png", // Generic event image
		Volume: handlers.FlexFloat(1000000),
		Markets: []handlers.RawGammaMarket{
			{
				ID: "market-trump", GroupItemTitle: "Trump", Image: "trump-photo.png",
				Active: true, Closed: false, OutcomePricesRaw: `["0.55"]`,
			},
			{
				ID: "market-harris", GroupItemTitle: "Harris", Image: "harris-photo.png",
				Active: true, Closed: false, OutcomePricesRaw: `["0.35"]`,
			},
			{
				ID: "market-desantis", GroupItemTitle: "DeSantis", Image: "desantis-photo.png",
				Active: true, Closed: false, OutcomePricesRaw: `["0.10"]`,
			},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "group", result.DisplayType)
	assert.Len(t, result.Outcomes, 3)

	// Verify each outcome has its own market's image (not the generic event image)
	assert.Equal(t, "trump-photo.png", result.Outcomes[0].Image)   // Trump (highest probability)
	assert.Equal(t, "harris-photo.png", result.Outcomes[1].Image)  // Harris
	assert.Equal(t, "desantis-photo.png", result.Outcomes[2].Image) // DeSantis (lowest probability)

	// Verify images are NOT the generic event image
	for _, outcome := range result.Outcomes {
		assert.NotEqual(t, "election-generic.png", outcome.Image, "Outcome should not have generic event image")
	}
}

func TestBinaryEventPreservesMarketImage(t *testing.T) {
	// Binary crypto event where market has a specific chart image.
	// Both Yes and No outcomes should have that image.
	rawEvent := handlers.RawGammaEvent{
		ID:     "btc-prediction",
		Title:  "Will BTC hit 100k by EOY?",
		Image:  "generic-crypto.png", // Event image
		Volume: handlers.FlexFloat(50000),
		Markets: []handlers.RawGammaMarket{
			{
				ID: "market-btc", Image: "btc-chart.png", // Market-specific image
				Active: true, Closed: false, OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.65", "0.35"]`,
			},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "binary", result.DisplayType)
	assert.Len(t, result.Outcomes, 2)

	// Both outcomes should have the market's specific image, not the generic event image
	assert.Equal(t, "btc-chart.png", result.Outcomes[0].Image) // Yes
	assert.Equal(t, "btc-chart.png", result.Outcomes[1].Image) // No

	// Verify they don't have the generic event image
	assert.NotEqual(t, "generic-crypto.png", result.Outcomes[0].Image)
	assert.NotEqual(t, "generic-crypto.png", result.Outcomes[1].Image)
}

func TestGroupEventImageNotSwappedBySorting(t *testing.T) {
	// 4 markets with different images and probabilities.
	// After sorting by probability DESC, each outcome still has the image from its own original market.
	rawEvent := handlers.RawGammaEvent{
		ID:     "tech-stocks",
		Title:  "Which Tech Stock Will Perform Best in 2024?",
		Volume: handlers.FlexFloat(200000),
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "Apple", Image: "apple-logo.png", OutcomePricesRaw: `["0.10"]`},  // 4th place
			{ID: "m2", GroupItemTitle: "Google", Image: "google-logo.png", OutcomePricesRaw: `["0.40"]`}, // 2nd place
			{ID: "m3", GroupItemTitle: "Tesla", Image: "tesla-logo.png", OutcomePricesRaw: `["0.05"]`},  // 5th place (excluded from top 5)
			{ID: "m4", GroupItemTitle: "Meta", Image: "meta-logo.png", OutcomePricesRaw: `["0.25"]`},    // 3rd place
			{ID: "m5", GroupItemTitle: "Microsoft", Image: "microsoft-logo.png", OutcomePricesRaw: `["0.20"]`}, // 4th place (but after Apple)
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "group", result.DisplayType)
	assert.Len(t, result.Outcomes, 5) // All markets included (no zero probability exclusions)

	// Verify sorting by probability DESC and image preservation
	assert.Equal(t, "Google", result.Outcomes[0].Label)     // 0.40 - highest
	assert.Equal(t, "google-logo.png", result.Outcomes[0].Image)

	assert.Equal(t, "Meta", result.Outcomes[1].Label)       // 0.25
	assert.Equal(t, "meta-logo.png", result.Outcomes[1].Image)

	assert.Equal(t, "Microsoft", result.Outcomes[2].Label)  // 0.20
	assert.Equal(t, "microsoft-logo.png", result.Outcomes[2].Image)

	assert.Equal(t, "Apple", result.Outcomes[3].Label)      // 0.10
	assert.Equal(t, "apple-logo.png", result.Outcomes[3].Image)

	assert.Equal(t, "Tesla", result.Outcomes[4].Label)      // 0.05 - lowest
	assert.Equal(t, "tesla-logo.png", result.Outcomes[4].Image)
}

// =============================================================================
// PRIMARY MARKET SELECTION TESTS
// =============================================================================

func TestSportsMoneylineNotFirst(t *testing.T) {
	// Raw event has spread market at Markets[0] (higher liquidity), moneyline at Markets[2].
	// Outcomes must use moneyline market data and images.
	rawEvent := handlers.RawGammaEvent{
		ID:       "nhl-game-1",
		Title:    "Maple Leafs vs Canadiens",
		Category: "Sports",
		GameID:   handlers.FlexString("12345"),
		Tags:     []handlers.RawGammaTag{{Slug: "nhl"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID: "spread-market", SportsMarketType: "spread", Image: "spread-generic.png",
				Liquidity: handlers.FlexFloat(100000), // Higher liquidity
				OutcomesRaw: `["Maple Leafs -1.5", "Canadiens +1.5"]`, OutcomePricesRaw: `["0.52", "0.48"]`,
			},
			{
				ID: "total-market", SportsMarketType: "total", Image: "total-generic.png",
				Liquidity: handlers.FlexFloat(50000),
				OutcomesRaw: `["Over 6.5", "Under 6.5"]`, OutcomePricesRaw: `["0.50", "0.50"]`,
			},
			{
				ID: "moneyline-market", SportsMarketType: "moneyline", Image: "moneyline-specific.png",
				Liquidity: handlers.FlexFloat(75000), // Lower liquidity than spread but is moneyline
				OutcomesRaw: `["Maple Leafs", "Canadiens"]`, OutcomePricesRaw: `["0.55", "0.45"]`,
			},
		},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, map[string]db.League{})

	assert.NotNil(t, result)
	assert.Equal(t, "sports", result.DisplayType)
	assert.Len(t, result.Outcomes, 2)

	// Outcomes should use moneyline market data (not spread or total)
	assert.Equal(t, "Maple Leafs", result.Outcomes[0].Label)
	assert.Equal(t, 0.55, result.Outcomes[0].Probability)
	assert.Equal(t, "Canadiens", result.Outcomes[1].Label)
	assert.Equal(t, 0.45, result.Outcomes[1].Probability)

	// Outcomes should have moneyline market's liquidity and volume (not spread's)
	assert.Equal(t, 75000.0, result.Outcomes[0].Liquidity)
	assert.Equal(t, 75000.0, result.Outcomes[1].Liquidity)
}

func TestSportsMoneylineUsedForDisplayData(t *testing.T) {
	// Same setup as above; DisplayData.Participants are built from moneyline outcomes,
	// not from the higher-liquidity spread market.
	rawEvent := handlers.RawGammaEvent{
		ID:       "nba-game-1",
		Title:    "Lakers vs Warriors",
		Category: "Sports",
		GameID:   handlers.FlexString("67890"),
		Tags:     []handlers.RawGammaTag{{Slug: "nba"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID: "spread-market", SportsMarketType: "spread",
				Liquidity: handlers.FlexFloat(200000), // Much higher liquidity
				OutcomesRaw: `["Lakers -2.5", "Warriors +2.5"]`, OutcomePricesRaw: `["0.50", "0.50"]`,
			},
			{
				ID: "moneyline-market", SportsMarketType: "moneyline",
				Liquidity: handlers.FlexFloat(50000), // Lower liquidity
				OutcomesRaw: `["Lakers", "Warriors"]`, OutcomePricesRaw: `["0.60", "0.40"]`,
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}

	result := handlers.TransformEventV2(rawEvent, map[string]db.PlyMktTeam{}, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "sports", result.DisplayType)
	assert.NotNil(t, result.DisplayData)
	assert.Equal(t, "versus_match", result.DisplayData.Type)
	assert.Len(t, result.DisplayData.Participants, 2)

	// DisplayData should use moneyline team names (not spread names like "Lakers -2.5")
	assert.Equal(t, "Lakers", result.DisplayData.Participants[0].Name)
	assert.Equal(t, "Warriors", result.DisplayData.Participants[1].Name)
}
