package handlers

import (
	"encoding/json"
	"strconv"
)

// =============================================================================
// FLEXIBLE TYPES (Handle Gamma API inconsistencies)
// =============================================================================

// FlexFloat handles JSON fields that can be either a number or a string.
// The Gamma API inconsistently returns some numeric fields as strings.
type FlexFloat float64

// FlexString handles JSON fields that can be either a string or a number.
// The Gamma API inconsistently returns some string fields as numbers.
type FlexString string

// UnmarshalJSON implements custom unmarshaling for FlexFloat.
func (f *FlexFloat) UnmarshalJSON(data []byte) error {
	// Try as number first
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = FlexFloat(num)
		return nil
	}

	// Try as string
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str == "" {
			*f = 0
			return nil
		}
		num, err := strconv.ParseFloat(str, 64)
		if err != nil {
			*f = 0
			return nil // Don't fail, just use zero
		}
		*f = FlexFloat(num)
		return nil
	}

	// Default to zero for null or other
	*f = 0
	return nil
}

// Float64 returns the FlexFloat as a standard float64.
func (f FlexFloat) Float64() float64 {
	return float64(f)
}

// UnmarshalJSON implements custom unmarshaling for FlexString.
func (f *FlexString) UnmarshalJSON(data []byte) error {
	// Try as string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*f = FlexString(str)
		return nil
	}

	// Try as number (convert to string)
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = FlexString(strconv.FormatFloat(num, 'f', -1, 64))
		return nil
	}

	// Default to empty string for null or other
	*f = ""
	return nil
}

// String returns the FlexString as a standard string.
func (f FlexString) String() string {
	return string(f)
}

// =============================================================================
// RAW INPUT STRUCTS (Matches Gamma API exactly)
// =============================================================================

// RawGammaMarket represents a single market from the Polymarket Gamma API.
// IMPORTANT: OutcomesRaw and OutcomePricesRaw are JSON strings, NOT arrays!
// Example: "[\"Yes\", \"No\"]" and "[\"0.52\", \"0.48\"]"
type RawGammaMarket struct {
	ID               string    `json:"id"`
	Question         string    `json:"question"`
	GroupItemTitle   string    `json:"groupItemTitle"` // Candidate Name (e.g. "Trump")
	OutcomesRaw      string    `json:"outcomes"`       // JSON string: "[\"Yes\", \"No\"]"
	OutcomePricesRaw string    `json:"outcomePrices"`  // JSON string: "[\"0.52\", \"0.48\"]"
	TeamAID          string    `json:"teamAID"`
	TeamBID          string    `json:"teamBID"`
	BestBid          FlexFloat `json:"bestBid"`
	BestAsk          FlexFloat `json:"bestAsk"`
	Liquidity        FlexFloat `json:"liquidity"`
	Volume24hr       FlexFloat `json:"volume24hr"`
	Image            string    `json:"image"`
	SportsMarketType string    `json:"sportsMarketType"`
	Active           bool      `json:"active"`
	Closed           bool      `json:"closed"`

	// Additional trading data
	EndDate            string    `json:"endDate"`
	GameStartTime      string    `json:"gameStartTime"`
	Volume24hrClob     FlexFloat `json:"volume24hrClob"`
	LiquidityClob      FlexFloat `json:"liquidityClob"`
	OneDayPriceChange  FlexFloat `json:"oneDayPriceChange"`
	OneHourPriceChange FlexFloat `json:"oneHourPriceChange"`
	LastTradePrice     FlexFloat `json:"lastTradePrice"`
	ClosedTime         string    `json:"closedTime"`
	FinishedTimestamp  string    `json:"finishedTimestamp"`
	ClobTokenIds       string    `json:"clobTokenIds"`
}

// RawGammaEvent represents an event from the Polymarket Gamma API.
type RawGammaEvent struct {
	ID                    string           `json:"id"`
	Title                 string           `json:"title"`
	Slug                  string           `json:"slug"`
	Category              string           `json:"category"`
	NegRisk               bool             `json:"negRisk"`
	Active                bool             `json:"active"`
	UmaResolutionStatuses string           `json:"umaResolutionStatuses,omitempty"` // Nullable
	UmaResolutionStatus   string           `json:"umaResolutionStatus,omitempty"`   // Nullable
	Markets               []RawGammaMarket `json:"markets"`
	Image                 string           `json:"image"`
	Icon                  string           `json:"icon"`
	Description           string           `json:"description"`
	Volume                FlexFloat        `json:"volume"`
	Volume24hr            FlexFloat        `json:"volume24hr"`
	Teams                 []RawGammaTeam   `json:"teams"`
	GameID                FlexString       `json:"gameId"`
	Liquidity             FlexFloat        `json:"liquidity"`
	Tags                  []RawGammaTag    `json:"tags"`

	// Dates and lifecycle
	EndDate    string `json:"endDate"`
	ClosedTime string `json:"closedTime"`

	// Live game status (from API response)
	Live           bool      `json:"live"`   // Whether game is currently broadcasting
	Ended          bool      `json:"ended"`  // Whether game has ended
	Score          string    `json:"score"`  // Current score, e.g., "49-67"
	Period         string    `json:"period"` // Current period, e.g., "FT", "2H"
	LiquidityClob  FlexFloat `json:"liquidityClob"`
	Volume24hrClob FlexFloat `json:"volume24hrClob"`
	StartDate      string    `json:"startDate"`
	StartTime      string    `json:"startTime"`
}

// RawGammaTeam represents a team from the Polymarket Gamma API teams field.
type RawGammaTeam struct {
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
	Logo         string `json:"logo"`
}

// RawGammaTag represents a tag from the Polymarket Gamma API tags field.
type RawGammaTag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Slug  string `json:"slug"`
}

// =============================================================================
// CLEAN OUTPUT STRUCTS (Render-ready for Frontend)
// =============================================================================

// CleanEvent is the sanitized, render-ready event sent to the frontend.
type CleanEvent struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Ticker string `json:"ticker"` // e.g. "TRUMP" or "KC vs BAL"
	Layout string `json:"layout"` // "POLL" | "SPORTS" | "BINARY"
	Image  string `json:"image"`

	// Status Signals
	IsLive      bool   `json:"isLive"`
	StatusBadge string `json:"statusBadge"` // "HOT", "DISPUTED", or ""

	// The "Alpha" Metrics
	Stats EventStats `json:"stats"`

	// Pre-Sorted Display Data
	DisplayData       DisplayData `json:"displayData"`
	Score             string      `json:"score,omitempty"`
	Period            string      `json:"period,omitempty"`
	FinishedTimestamp string      `json:"finishedTimestamp,omitempty"`
	Volume24hrClob    float64     `json:"volume24hrClob,omitempty"`
	Liquidity         float64     `json:"liquidity,omitempty"`
}

// EventStats contains trading metrics for the event.
type EventStats struct {
	VolumeUSD     string `json:"volumeUSD"`     // "$1.2M"
	SpreadBP      int    `json:"spreadBP"`      // Spread in basis points
	IsWhaleAction bool   `json:"isWhaleAction"` // Volatility signal
}

// DisplayData contains layout-specific display information.
type DisplayData struct {
	// POLLS: Sorted list (Winner first). BINARY: Just "Yes"/"No".
	Outcomes []OutcomeObj `json:"outcomes"`

	// SPORTS: Structured Matchup (nil for non-sports)
	Teams *MatchupObj `json:"teams,omitempty"`
}

// OutcomeObj represents a single outcome for POLL or BINARY layouts.
type OutcomeObj struct {
	Label    string  `json:"label"`
	Price    float64 `json:"price"`
	Image    string  `json:"image"`
	IsWinner bool    `json:"isWinner"` // Visual highlight flag
}

// MatchupObj represents a sports matchup between two teams.
type MatchupObj struct {
	Home TeamObj `json:"home"`
	Away TeamObj `json:"away"`
}

// TeamObj represents a team in a sports matchup.
type TeamObj struct {
	Label      string  `json:"label"`
	Price      float64 `json:"price"`
	IsFavorite bool    `json:"isFavorite"`
	TeamID     string  `json:"teamId"`
}

// =============================================================================
// V2 EVENT RESPONSE TYPES (New standardized schema for frontend)
// =============================================================================

type V2EventResponse struct {
	Events []V2Event `json:"events"`
}

type V2Event struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Subtitle      string  `json:"subtitle"`
	Image         string  `json:"image"`
	OriginalImage string  `json:"originalImage"`
	TotalVolume   float64 `json:"totalVolume"`
	DisplayType   string  `json:"displayType"` // "binary" | "group" | "sports"

	// Event-level trading data
	EndDate    string  `json:"endDate,omitempty"`
	Liquidity  float64 `json:"liquidity"`
	Volume24hr float64 `json:"volume24hr"`

	// Status & Activity
	IsLive      bool   `json:"isLive"`      // Whether event is currently active
	StatusBadge string `json:"statusBadge"` // "HOT", "DISPUTED", or ""

	// Trading Statistics
	Stats V2EventStats `json:"stats"`

	// Market Data
	Outcomes    []V2Outcome    `json:"outcomes"`
	DisplayData *V2DisplayData `json:"displayData,omitempty"`

	// Additional event-level data
	Score             string  `json:"score,omitempty"`
	Period            string  `json:"period,omitempty"`
	FinishedTimestamp string  `json:"finishedTimestamp,omitempty"`
	Volume24hrClob    float64 `json:"volume24hrClob,omitempty"`
	LiquidityClob     float64 `json:"liquidityClob,omitempty"`
	StartDate         string  `json:"startDate,omitempty"`
	StartTime         string  `json:"startTime,omitempty"`
}

type V2EventStats struct {
	VolumeUSD     string `json:"volumeUSD"`     // Formatted volume (e.g., "$1.2M")
	SpreadBP      int    `json:"spreadBP"`      // Spread in basis points
	IsWhaleAction bool   `json:"isWhaleAction"` // High volume + tight spread signal
}

type V2Outcome struct {
	MarketID         string  `json:"marketId"`
	Label            string  `json:"label"`
	Probability      float64 `json:"probability"`
	Price            float64 `json:"price,omitempty"`      // Raw price (complement to probability)
	BestBid          float64 `json:"bestBid,omitempty"`    // Best bid price
	BestAsk          float64 `json:"bestAsk,omitempty"`    // Best ask price
	Liquidity        float64 `json:"liquidity,omitempty"`  // Market liquidity
	Volume24hr       float64 `json:"volume24hr,omitempty"` // 24hr volume for this market
	Status           string  `json:"status"`               // "active" | "resolved"
	Color            string  `json:"color"`
	Image            string  `json:"image"`
	SportsMarketType string  `json:"sportsMarketType,omitempty"`

	// Additional market-level trading data
	EndDate            string  `json:"endDate,omitempty"`
	GameStartTime      string  `json:"gameStartTime,omitempty"`
	Volume24hrClob     float64 `json:"volume24hrClob,omitempty"`
	LiquidityClob      float64 `json:"liquidityClob,omitempty"`
	OneDayPriceChange  float64 `json:"oneDayPriceChange"`
	OneHourPriceChange float64 `json:"oneHourPriceChange"`
	LastTradePrice     float64 `json:"lastTradePrice"`
	Spread             float64 `json:"spread"`
	ClobTokenIds       string  `json:"clobTokenIds,omitempty"`
}

type V2DisplayData struct {
	Type           string          `json:"type"` // "versus_match" | "single_entity" | "tournament"
	Participants   []V2Participant `json:"participants"`
	CompositeImage string          `json:"compositeImage,omitempty"`
}

type V2Participant struct {
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
	Color    string `json:"color,omitempty"`
	Role     string `json:"role"` // "home" | "away" | "player_1" | "player_2"
}
