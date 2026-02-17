package handlers_test

import (
	"testing"

	"github.com/samucap/orion2.0/handlers"
	"github.com/stretchr/testify/assert"
)

// TestZombieMarketDropped verifies that a market with price 1.00 and active status
// is dropped (returns nil) because it's a zombie.
func TestZombieMarketDropped(t *testing.T) {
	// Create a raw event with a single zombie market (price = 1.00)
	rawEvent := handlers.RawGammaEvent{
		ID:                  "test-event-1",
		Title:               "Test Zombie Event",
		Slug:                "test-zombie-event",
		Category:            "Crypto",
		NegRisk:             false,
		Active:              true,
		UmaResolutionStatus: nil, // Not disputed
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-1",
				Question:         "Will this resolve yes?",
				GroupItemTitle:   "",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["1.00", "0.00"]`, // Zombie: price at 100%
				BestBid:          handlers.FlexFloat(0.99),
				BestAsk:          handlers.FlexFloat(1.00),
				Liquidity:        handlers.FlexFloat(1000),
				Volume24hr:       handlers.FlexFloat(5000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	// Assert: should be nil because all markets are zombies
	assert.Nil(t, result, "Zombie market with price 1.00 should be dropped")
}

// TestDisputedMarketKept verifies that a market with price 1.00 but disputed status
// is kept (not dropped) because disputed markets should be preserved.
func TestDisputedMarketKept(t *testing.T) {
	// Create a raw event with a zombie market but disputed status
	disputedStatus := "disputed"
	rawEvent := handlers.RawGammaEvent{
		ID:                  "test-event-2",
		Title:               "Test Disputed Event",
		Slug:                "test-disputed-event",
		Category:            "Crypto",
		NegRisk:             false,
		Active:              true,
		UmaResolutionStatus: &disputedStatus, // Disputed!
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-2",
				Question:         "Will this resolve yes?",
				GroupItemTitle:   "",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["1.00", "0.00"]`, // Zombie price, but disputed
				BestBid:          handlers.FlexFloat(0.99),
				BestAsk:          handlers.FlexFloat(1.00),
				Liquidity:        handlers.FlexFloat(1000),
				Volume24hr:       handlers.FlexFloat(5000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	// Assert: should NOT be nil because disputed markets are kept
	assert.NotNil(t, result, "Disputed market should be kept even with zombie price")
	assert.Equal(t, "test-event-2", result.ID)
	assert.Equal(t, "DISPUTED", result.StatusBadge)
	assert.Equal(t, handlers.LayoutBinary, result.Layout)
}

// TestSportsLayoutWithTeams verifies that a sports event gets the correct layout
// and has the Teams object populated with correct favorite flag.
func TestSportsLayoutWithTeams(t *testing.T) {
	// Create a raw sports event with two teams
	rawEvent := handlers.RawGammaEvent{
		ID:                  "test-event-3",
		Title:               "Chiefs vs Ravens",
		Slug:                "chiefs-vs-ravens",
		Category:            "Sports",
		NegRisk:             false,
		Active:              true,
		UmaResolutionStatus: nil,
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-3",
				Question:         "Who will win?",
				GroupItemTitle:   "",
				OutcomesRaw:      `["Kansas City Chiefs", "Baltimore Ravens"]`,
				OutcomePricesRaw: `["0.45", "0.55"]`, // Ravens are favorite
				TeamAID:          "team-kc",
				TeamBID:          "team-bal",
				BestBid:          handlers.FlexFloat(0.44),
				BestAsk:          handlers.FlexFloat(0.46),
				Liquidity:        handlers.FlexFloat(50000),
				Volume24hr:       handlers.FlexFloat(120000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	// Assert: should have SPORTS layout
	assert.NotNil(t, result)
	assert.Equal(t, handlers.LayoutSports, result.Layout)
	assert.Equal(t, "test-event-3", result.ID)

	// Assert: Teams should be populated
	assert.NotNil(t, result.DisplayData.Teams, "Sports layout should have Teams populated")

	// Assert: Home team (index 0) details
	assert.Equal(t, "Kansas City Chiefs", result.DisplayData.Teams.Home.Label)
	assert.Equal(t, 0.45, result.DisplayData.Teams.Home.Price)
	assert.Equal(t, "team-kc", result.DisplayData.Teams.Home.TeamID)
	assert.False(t, result.DisplayData.Teams.Home.IsFavorite, "Chiefs should not be favorite (lower price)")

	// Assert: Away team (index 1) details
	assert.Equal(t, "Baltimore Ravens", result.DisplayData.Teams.Away.Label)
	assert.Equal(t, 0.55, result.DisplayData.Teams.Away.Price)
	assert.Equal(t, "team-bal", result.DisplayData.Teams.Away.TeamID)
	assert.True(t, result.DisplayData.Teams.Away.IsFavorite, "Ravens should be favorite (higher price)")

	// Assert: Ticker format
	assert.Equal(t, "Kansas City Chiefs vs Baltimore Ravens", result.Ticker)
}

// TestPollLayoutSorting verifies that POLL events sort candidates by price DESC
// and mark the first one as winner.
func TestPollLayoutSorting(t *testing.T) {
	// Create a poll/election event with multiple candidates
	rawEvent := handlers.RawGammaEvent{
		ID:                  "test-event-4",
		Title:               "Presidential Election Winner",
		Slug:                "presidential-election",
		Category:            "Politics",
		NegRisk:             true, // negRisk makes it a poll
		Active:              true,
		UmaResolutionStatus: nil,
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-biden",
				Question:         "Will Biden win?",
				GroupItemTitle:   "Biden",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.25", "0.75"]`,
				Liquidity:        handlers.FlexFloat(10000),
				Volume24hr:       handlers.FlexFloat(50000),
			},
			{
				ID:               "market-trump",
				Question:         "Will Trump win?",
				GroupItemTitle:   "Trump",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.60", "0.40"]`, // Highest price
				Liquidity:        handlers.FlexFloat(15000),
				Volume24hr:       handlers.FlexFloat(80000),
			},
			{
				ID:               "market-harris",
				Question:         "Will Harris win?",
				GroupItemTitle:   "Harris",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.15", "0.85"]`,
				Liquidity:        handlers.FlexFloat(8000),
				Volume24hr:       handlers.FlexFloat(30000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	// Assert: should have POLL layout
	assert.NotNil(t, result)
	assert.Equal(t, handlers.LayoutPoll, result.Layout)

	// Assert: Outcomes should be sorted by price DESC
	assert.Len(t, result.DisplayData.Outcomes, 3)

	// First should be Trump (highest price 0.60)
	assert.Equal(t, "Trump", result.DisplayData.Outcomes[0].Label)
	assert.Equal(t, 0.60, result.DisplayData.Outcomes[0].Price)
	assert.True(t, result.DisplayData.Outcomes[0].IsWinner, "Highest price should be marked as winner")

	// Second should be Biden (0.25)
	assert.Equal(t, "Biden", result.DisplayData.Outcomes[1].Label)
	assert.Equal(t, 0.25, result.DisplayData.Outcomes[1].Price)
	assert.False(t, result.DisplayData.Outcomes[1].IsWinner)

	// Third should be Harris (0.15)
	assert.Equal(t, "Harris", result.DisplayData.Outcomes[2].Label)
	assert.Equal(t, 0.15, result.DisplayData.Outcomes[2].Price)
	assert.False(t, result.DisplayData.Outcomes[2].IsWinner)
}

// TestBinaryLayout verifies that a simple binary event gets proper layout.
func TestBinaryLayout(t *testing.T) {
	rawEvent := handlers.RawGammaEvent{
		ID:                  "test-event-5",
		Title:               "Will BTC hit 100k?",
		Slug:                "btc-100k",
		Category:            "Crypto",
		NegRisk:             false,
		Active:              true,
		UmaResolutionStatus: nil,
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-5",
				Question:         "Will BTC hit 100k?",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.65", "0.35"]`,
				BestBid:          handlers.FlexFloat(0.64),
				BestAsk:          handlers.FlexFloat(0.66),
				Liquidity:        handlers.FlexFloat(25000),
				Volume24hr:       handlers.FlexFloat(75000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	assert.NotNil(t, result)
	assert.Equal(t, handlers.LayoutBinary, result.Layout)
	assert.Len(t, result.DisplayData.Outcomes, 2)
	assert.Equal(t, "Yes", result.DisplayData.Outcomes[0].Label)
	assert.Equal(t, 0.65, result.DisplayData.Outcomes[0].Price)
	assert.Equal(t, "No", result.DisplayData.Outcomes[1].Label)
	assert.Equal(t, 0.35, result.DisplayData.Outcomes[1].Price)
	assert.Nil(t, result.DisplayData.Teams, "Binary layout should not have Teams")
}

// TestStatsCalculation verifies that stats are correctly calculated.
func TestStatsCalculation(t *testing.T) {
	rawEvent := handlers.RawGammaEvent{
		ID:       "test-event-6",
		Title:    "High Volume Event",
		Slug:     "high-volume",
		Category: "Crypto",
		Active:   true,
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-6",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.50", "0.50"]`,
				BestBid:          handlers.FlexFloat(0.49),
				BestAsk:          handlers.FlexFloat(0.51),
				Liquidity:        handlers.FlexFloat(100000),
				Volume24hr:       handlers.FlexFloat(1500000), // $1.5M
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	assert.NotNil(t, result)
	assert.Equal(t, "$1.5M", result.Stats.VolumeUSD)
	assert.Equal(t, 200, result.Stats.SpreadBP) // (0.51 - 0.49) * 10000 = 200 BP
	assert.Equal(t, "HOT", result.StatusBadge)  // Volume > 500k
}

// TestZombieAtLowPrice verifies that markets at 0.01 (1%) are also filtered.
func TestZombieAtLowPrice(t *testing.T) {
	rawEvent := handlers.RawGammaEvent{
		ID:       "test-event-7",
		Title:    "Nearly Dead Market",
		Slug:     "nearly-dead",
		Category: "Crypto",
		Active:   true,
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-7",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.01", "0.99"]`, // Zombie: Yes price at 1%
				BestBid:          handlers.FlexFloat(0.01),
				BestAsk:          handlers.FlexFloat(0.02),
				Liquidity:        handlers.FlexFloat(500),
				Volume24hr:       handlers.FlexFloat(1000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	// Assert: should be nil because market is zombie (price <= 0.01)
	assert.Nil(t, result, "Market with price 0.01 should be dropped as zombie")
}

// TestAnsweringStatusKept verifies that "answering" status (like disputed) keeps markets.
func TestAnsweringStatusKept(t *testing.T) {
	answeringStatus := "answering"
	rawEvent := handlers.RawGammaEvent{
		ID:                  "test-event-8",
		Title:               "Answering Event",
		Slug:                "answering-event",
		Category:            "Crypto",
		Active:              true,
		UmaResolutionStatus: &answeringStatus,
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "market-8",
				OutcomesRaw:      `["Yes", "No"]`,
				OutcomePricesRaw: `["0.99", "0.01"]`, // Would be zombie
				Liquidity:        handlers.FlexFloat(1000),
				Volume24hr:       handlers.FlexFloat(5000),
			},
		},
	}

	result := handlers.TransformEvent(rawEvent)

	assert.NotNil(t, result, "Market with 'answering' status should be kept")
	assert.Equal(t, "DISPUTED", result.StatusBadge)
}
