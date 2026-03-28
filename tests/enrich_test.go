package handlers_test

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestEnrichSportsVersusMatch(t *testing.T) {
	// Test enrichment for a head-to-head match
	rawEvent := handlers.RawGammaEvent{
		ID:   "test-match",
		Tags: []handlers.RawGammaTag{{Slug: "nba"}}, // Will match league with "home" ordering
		Markets: []handlers.RawGammaMarket{
			{
				OutcomesRaw:      `["Los Angeles Lakers", "Boston Celtics"]`,
				OutcomePricesRaw: `["0.55", "0.45"]`,
			},
		},
	}

	// Mock league data
	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}

	// Mock team data
	teamsByName := map[string]db.PlyMktTeam{
		"los angeles lakers": {Name: "Los Angeles Lakers", Logo: "lakers-logo.png", Color: "#552583"},
		"boston celtics":     {Name: "Boston Celtics", Logo: "celtics-logo.png", Color: "#007A33"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Home team (first in outcomes, ordering="home")
	home := result.Participants[0]
	assert.Equal(t, "Los Angeles Lakers", home.Name)
	assert.Equal(t, "home", home.Role)
	assert.Equal(t, "lakers-logo.png", home.ImageURL)
	assert.Equal(t, "#552583", home.Color)

	// Away team (second in outcomes, ordering="home")
	away := result.Participants[1]
	assert.Equal(t, "Boston Celtics", away.Name)
	assert.Equal(t, "away", away.Role)
	assert.Equal(t, "celtics-logo.png", away.ImageURL)
	assert.Equal(t, "#007A33", away.Color)
}

func TestEnrichSportsVersusMatchAwayOrdering(t *testing.T) {
	// Test enrichment with "away" ordering (first outcome = away team)
	rawEvent := handlers.RawGammaEvent{
		ID:   "test-match",
		Tags: []handlers.RawGammaTag{{Slug: "mlb"}}, // MLB has "away" ordering
		Markets: []handlers.RawGammaMarket{
			{
				OutcomesRaw:      `["Boston Red Sox", "New York Yankees"]`,
				OutcomePricesRaw: `["0.45", "0.55"]`,
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"mlb": {Sport: "mlb", Ordering: "away"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"boston red sox":   {Name: "Boston Red Sox", Logo: "redsox-logo.png"},
		"new york yankees": {Name: "New York Yankees", Logo: "yankees-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Away team (first in outcomes, ordering="away")
	away := result.Participants[0]
	assert.Equal(t, "Boston Red Sox", away.Name)
	assert.Equal(t, "away", away.Role)

	// Home team (second in outcomes, ordering="away")
	home := result.Participants[1]
	assert.Equal(t, "New York Yankees", home.Name)
	assert.Equal(t, "home", home.Role)
}

func TestEnrichSportsGroupEvent(t *testing.T) {
	// Test enrichment for a tournament/futures event
	rawEvent := handlers.RawGammaEvent{
		ID:   "nba-champions",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "nba"}},
		Markets: []handlers.RawGammaMarket{
			{GroupItemTitle: "Boston Celtics", OutcomePricesRaw: `["0.25"]`},
			{GroupItemTitle: "Milwaukee Bucks", OutcomePricesRaw: `["0.20"]`},
			{GroupItemTitle: "Denver Nuggets", OutcomePricesRaw: `["0.30"]`},
		},
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"boston celtics":  {Name: "Boston Celtics", Logo: "celtics-logo.png"},
		"milwaukee bucks": {Name: "Milwaukee Bucks", Logo: "bucks-logo.png"},
		"denver nuggets":  {Name: "Denver Nuggets", Logo: "nuggets-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "tournament", result.Type)
	assert.Len(t, result.Participants, 3) // Top 3 by probability

	// Should be sorted by probability (Denver first)
	assert.Equal(t, "Denver Nuggets", result.Participants[0].Name)
	assert.Equal(t, "player_1", result.Participants[0].Role)
	assert.Equal(t, "nuggets-logo.png", result.Participants[0].ImageURL)

	assert.Equal(t, "Boston Celtics", result.Participants[1].Name)
	assert.Equal(t, "celtics-logo.png", result.Participants[1].ImageURL)

	assert.Equal(t, "Milwaukee Bucks", result.Participants[2].Name)
	assert.Equal(t, "bucks-logo.png", result.Participants[2].ImageURL)
}

func TestEnrichNoLeagueMatch(t *testing.T) {
	// When no league matches (e.g. esports, unconfigured sport), we still return DisplayData
	// with default ordering (first=home, second=away) so matchup data and API logos can display.
	rawEvent := handlers.RawGammaEvent{
		ID:   "unknown-sport",
		Tags: []handlers.RawGammaTag{{Slug: "unknown-sport-slug"}},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Team A", "Team B"]`},
		},
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)
	assert.Equal(t, "Team A", result.Participants[0].Name)
	assert.Equal(t, "home", result.Participants[0].Role)
	assert.Equal(t, "Team B", result.Participants[1].Name)
	assert.Equal(t, "away", result.Participants[1].Role)
}

func TestEnrichMissingTeamData(t *testing.T) {
	// Test fallback when team data is missing: unknown team gets market image
	rawEvent := handlers.RawGammaEvent{
		ID:   "test-match",
		Tags: []handlers.RawGammaTag{{Slug: "nba"}},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Unknown Team", "Known Team"]`, Image: "matchup-image.png"},
		},
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"known team": {Name: "Known Team", Logo: "known-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Len(t, result.Participants, 2)

	// Known team should have logo from DB
	assert.Equal(t, "Known Team", result.Participants[1].Name)
	assert.Equal(t, "known-logo.png", result.Participants[1].ImageURL)

	// Unknown team should fall back to market image
	assert.Equal(t, "Unknown Team", result.Participants[0].Name)
	assert.Equal(t, "matchup-image.png", result.Participants[0].ImageURL)
}

func TestEnrichLeagueAwareLookup(t *testing.T) {
	// League-aware lookup: same team name in different leagues resolves to correct league's team
	rawEvent := handlers.RawGammaEvent{
		ID:   "esports-match",
		Tags: []handlers.RawGammaTag{{Slug: "esports"}},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Team Liquid", "G2 Esports"]`, OutcomePricesRaw: `["0.55", "0.45"]`},
		},
	}

	leaguesBySlug := map[string]db.League{
		"esports": {Sport: "esports", Ordering: "home"},
	}

	// Composite keys: esports|team liquid so event with tag "esports" finds this team
	teamsByName := map[string]db.PlyMktTeam{
		"team liquid":         {Name: "Team Liquid", League: "esports", Logo: "tl-esports.png", Color: "#00A0DC"},
		"esports|team liquid": {Name: "Team Liquid", League: "esports", Logo: "tl-esports.png", Color: "#00A0DC"},
		"g2 esports":          {Name: "G2 Esports", League: "esports", Logo: "g2-esports.png", Color: "#000000"},
		"esports|g2 esports":  {Name: "G2 Esports", League: "esports", Logo: "g2-esports.png", Color: "#000000"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)
	assert.Equal(t, "Team Liquid", result.Participants[0].Name)
	assert.Equal(t, "tl-esports.png", result.Participants[0].ImageURL)
	assert.Equal(t, "G2 Esports", result.Participants[1].Name)
	assert.Equal(t, "g2-esports.png", result.Participants[1].ImageURL)
}

func TestEnrichGroupMarketImageFallback(t *testing.T) {
	// When DB and raw team lookup fail for a group participant, fall back to that market's image
	rawEvent := handlers.RawGammaEvent{
		ID:   "futures-unknown",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "nba"}},
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "Known Team", OutcomePricesRaw: `["0.50"]`, Image: "known-market.png"},
			{ID: "m2", GroupItemTitle: "Unknown Team", OutcomePricesRaw: `["0.30"]`, Image: "team-market.png"},
		},
	}

	leaguesBySlug := map[string]db.League{"nba": {Sport: "nba", Ordering: "home"}}
	teamsByName := map[string]db.PlyMktTeam{
		"known team": {Name: "Known Team", Logo: "known-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "tournament", result.Type)
	assert.Len(t, result.Participants, 2)
	// First by probability is Known Team (0.50)
	assert.Equal(t, "Known Team", result.Participants[0].Name)
	assert.Equal(t, "known-logo.png", result.Participants[0].ImageURL)
	// Second is Unknown Team - falls back to its market image
	assert.Equal(t, "Unknown Team", result.Participants[1].Name)
	assert.Equal(t, "team-market.png", result.Participants[1].ImageURL)
}

func TestEnrichAbbreviationFallback(t *testing.T) {
	// Test that with the new simplified system, teams resolve via direct map keys
	// The old abbreviation fallback logic was removed in favor of DB-level fuzzy matching
	rawEvent := handlers.RawGammaEvent{
		ID:   "nhl-match",
		Tags: []handlers.RawGammaTag{{Slug: "nhl"}},
		Teams: []handlers.RawGammaTeam{
			{Name: "Colorado Avalanche", Abbreviation: "COL"},
			{Name: "Boston Bruins", Abbreviation: "BOS"},
		},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Colorado Avalanche", "Boston Bruins"]`},
		},
	}

	leaguesBySlug := map[string]db.League{"nhl": {Sport: "nhl", Ordering: "home"}}
	// In the new system, teams are resolved via QueryTeamsResolved which populates exact matches
	teamsByName := map[string]db.PlyMktTeam{
		"colorado avalanche":     {Name: "Colorado Avalanche", League: "nhl", Logo: "avalanche-logo.png"},
		"nhl|colorado avalanche": {Name: "Colorado Avalanche", League: "nhl", Logo: "avalanche-logo.png"},
		"boston bruins":          {Name: "Boston Bruins", League: "nhl", Logo: "bruins-logo.png"},
		"nhl|boston bruins":      {Name: "Boston Bruins", League: "nhl", Logo: "bruins-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Teams should resolve via direct key matches (populated by QueryTeamsResolved)
	avalanche := result.Participants[0]
	assert.Equal(t, "Colorado Avalanche", avalanche.Name)
	assert.Equal(t, "avalanche-logo.png", avalanche.ImageURL)

	bruins := result.Participants[1]
	assert.Equal(t, "Boston Bruins", bruins.Name)
	assert.Equal(t, "bruins-logo.png", bruins.ImageURL)
}

func TestQueryTeamsByLeague(t *testing.T) {
	// Load DB credentials from a local env file when present.
	candidates := []string{".env.test", "../.env.test"}
	loaded := ""
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if err := godotenv.Load(p); err != nil {
				t.Skipf("Skipping DB-dependent test: failed to load %s: %v", p, err)
			}
			loaded = p
			break
		}
	}
	if loaded == "" {
		t.Skip("Skipping DB-dependent test: .env.test not available in tests/ or project root")
	}

	// Initialize database connection for testing
	_, err := db.InitDB()
	if err != nil {
		t.Skipf("Database not available for testing: %v", err)
	}

	ctx := context.Background()

	// Test with a known league that should exist (like "fif" for FIFA)
	teams, err := db.QueryTeamsByLeague(ctx, "fif")
	assert.NoError(t, err)

	// Should find some teams for FIFA league
	assert.NotEmpty(t, teams, "Expected to find teams for fif league")

	// Check that the map contains expected keys
	// Look for Argentina which we know exists in fif league
	found := false
	for _, team := range teams {
		if team.Name == "Argentina" && team.League == "fif" {
			found = true
			assert.Equal(t, "arg", team.Abbreviation)
			assert.Equal(t, "https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/arg.png", team.Logo)

			// Verify key formats: both plain key and composite key should exist
			assert.Contains(t, teams, "argentina")     // plain normalized name
			assert.Contains(t, teams, "fif|argentina") // composite league|name key
			assert.Contains(t, teams, "arg")           // abbreviation
			assert.Contains(t, teams, "fif|arg")       // composite league|abbrev key
			break
		}
	}
	assert.True(t, found, "Expected to find Argentina in fif league teams")

	// Test with non-existent league
	teams, err = db.QueryTeamsByLeague(ctx, "nonexistent-league-12345")
	assert.NoError(t, err)
	assert.Empty(t, teams, "Expected no teams for non-existent league")
}

func TestTeamByLabelSimplified(t *testing.T) {
	// Test the new simplified TeamByLabel behavior - only direct map lookups
	rawEvent := handlers.RawGammaEvent{
		ID: "nhl-test",
		Teams: []handlers.RawGammaTeam{
			{Name: "Colorado Avalanche", Abbreviation: "COL"},
		},
	}

	// Mock teams map populated by QueryTeamsResolved (includes both league-qualified and plain keys)
	teamsByName := map[string]db.PlyMktTeam{
		"colorado avalanche":     {Name: "Colorado Avalanche", League: "nhl", Logo: "avalanche-logo.png"},
		"nhl|colorado avalanche": {Name: "Colorado Avalanche", League: "nhl", Logo: "avalanche-logo.png"},
		"avalanche":              {Name: "Avalanche", League: "nhl", Logo: "avalanche-logo.png"},
		"nhl|avalanche":          {Name: "Avalanche", League: "nhl", Logo: "avalanche-logo.png"},
		"bruins":                 {Name: "Bruins", League: "nhl", Logo: "bruins-logo.png"},
		"nhl|bruins":             {Name: "Bruins", League: "nhl", Logo: "bruins-logo.png"},
	}

	// Test league-qualified lookup (preferred)
	team := handlers.TeamByLabel(teamsByName, "nhl", "Colorado Avalanche", rawEvent)
	assert.NotNil(t, team, "Expected to find team for Colorado Avalanche via league-qualified lookup")
	assert.Equal(t, "Colorado Avalanche", team.Name)
	assert.Equal(t, "avalanche-logo.png", team.Logo)

	// Test fallback to plain key lookup
	team2 := handlers.TeamByLabel(teamsByName, "", "Avalanche", rawEvent) // No league specified
	assert.NotNil(t, team2, "Expected to find team for Avalanche via plain key lookup")
	assert.Equal(t, "Avalanche", team2.Name)
	assert.Equal(t, "avalanche-logo.png", team2.Logo)

	// Test that unmatched labels return nil
	team3 := handlers.TeamByLabel(teamsByName, "nhl", "Unknown Team", rawEvent)
	assert.Nil(t, team3, "Expected nil for unmatched team")
}

func TestEventLeagueSlugWithAliases(t *testing.T) {
	// Test that EventLeagueSlug resolves tag aliases (fifa -> fif, etc.)
	leaguesBySlug := map[string]db.League{
		"fif":  {Sport: "fif", LogoURLTemplate: "https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/{abbrev}.png"},
		"fifa": {Sport: "fif", LogoURLTemplate: "https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/{abbrev}.png"}, // aliased
	}

	rawEvent := handlers.RawGammaEvent{
		ID:   "fifa-world-cup",
		Tags: []handlers.RawGammaTag{{Slug: "fifa"}},
	}

	leagueSlug := handlers.EventLeagueSlug(rawEvent, leaguesBySlug)
	assert.Equal(t, "fifa", leagueSlug, "Expected fifa tag to resolve through alias")
}

func TestNegRiskGroupMarketLogos(t *testing.T) {
	// Test that each market in a sports_group event gets its own team logo
	rawEvent := handlers.RawGammaEvent{
		ID:   "nba-champions",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "nba"}},
		Markets: []handlers.RawGammaMarket{
			{GroupItemTitle: "Los Angeles Lakers", OutcomePricesRaw: `["0.25"]`},
			{GroupItemTitle: "Boston Celtics", OutcomePricesRaw: `["0.20"]`},
			{GroupItemTitle: "Denver Nuggets", OutcomePricesRaw: `["0.30"]`},
		},
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"los angeles lakers": {Name: "Los Angeles Lakers", Logo: "lakers-logo.png"},
		"boston celtics":     {Name: "Boston Celtics", Logo: "celtics-logo.png"},
		"denver nuggets":     {Name: "Denver Nuggets", Logo: "nuggets-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "tournament", result.Type)
	assert.Len(t, result.Participants, 3)

	// Each participant should have its own logo (sorted by probability DESC)
	assert.Equal(t, "Denver Nuggets", result.Participants[0].Name)
	assert.Equal(t, "nuggets-logo.png", result.Participants[0].ImageURL)
	assert.Equal(t, "Los Angeles Lakers", result.Participants[1].Name)
	assert.Equal(t, "lakers-logo.png", result.Participants[1].ImageURL)
	assert.Equal(t, "Boston Celtics", result.Participants[2].Name)
	assert.Equal(t, "celtics-logo.png", result.Participants[2].ImageURL)
}

func TestEsportsClassification(t *testing.T) {
	// Test that events tagged "esports" are classified as sports
	rawEvent := handlers.RawGammaEvent{
		ID:   "esports-match",
		Tags: []handlers.RawGammaTag{{Slug: "esports"}, {Slug: "cs2"}},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Team A", "Team B"]`},
		},
	}

	assert.True(t, handlers.IsSportsEvent(rawEvent), "Expected esports-tagged event to be classified as sports")
}

func TestCountryFlagsFallback(t *testing.T) {
	// Test that unmatched country labels still get country-flags
	leaguesBySlug := map[string]db.League{}
	teamsByName := map[string]db.PlyMktTeam{}
	rawEvent := handlers.RawGammaEvent{}

	// Test known country gets flag
	image := handlers.ResolveTeamImage("Argentina", nil, rawEvent, "", leaguesBySlug, "", "", teamsByName)
	assert.Equal(t, "https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/arg.png", image)

	// Test unknown label falls back to market image
	image2 := handlers.ResolveTeamImage("Unknown Team", nil, rawEvent, "", leaguesBySlug, "fallback.png", "", teamsByName)
	assert.Equal(t, "fallback.png", image2)
}

// =============================================================================
// SOCCER-SPECIFIC TESTS
// =============================================================================

func TestManUnitedAliasResolvesToManchesterUnited(t *testing.T) {
	// The API sends "Man United" but DB stores "Manchester United FC"
	rawEvent := handlers.RawGammaEvent{
		ID:   "epl-winner",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "epl"}},
		Markets: []handlers.RawGammaMarket{
			{GroupItemTitle: "Man United", OutcomePricesRaw: `["0.01"]`},
			{GroupItemTitle: "Man City", OutcomePricesRaw: `["0.28"]`},
			{GroupItemTitle: "Arsenal", OutcomePricesRaw: `["0.25"]`},
		},
	}

	// In the new system, QueryTeamsResolved populates the map with resolved aliases
	teamsByName := map[string]db.PlyMktTeam{
		"man united":               {Name: "Manchester United FC", League: "epl", Logo: "epl_manchester_united.png", Abbreviation: "mun"},
		"epl|man united":           {Name: "Manchester United FC", League: "epl", Logo: "epl_manchester_united.png", Abbreviation: "mun"},
		"manchester united fc":     {Name: "Manchester United FC", League: "epl", Logo: "epl_manchester_united.png", Abbreviation: "mun"},
		"epl|manchester united fc": {Name: "Manchester United FC", League: "epl", Logo: "epl_manchester_united.png", Abbreviation: "mun"},
		"man city":                 {Name: "Manchester City FC", League: "epl", Logo: "epl_manchester_city.png", Abbreviation: "mac"},
		"epl|man city":             {Name: "Manchester City FC", League: "epl", Logo: "epl_manchester_city.png", Abbreviation: "mac"},
		"arsenal":                  {Name: "Arsenal FC", League: "epl", Logo: "epl_arsenal.png", Abbreviation: "ars"},
		"epl|arsenal":              {Name: "Arsenal FC", League: "epl", Logo: "epl_arsenal.png", Abbreviation: "ars"},
	}

	// "Man United" should resolve via the alias key populated by QueryTeamsResolved
	team := handlers.TeamByLabel(teamsByName, "epl", "Man United", rawEvent)
	assert.NotNil(t, team, "Expected to find team for Man United via alias resolution")
	assert.Equal(t, "Manchester United FC", team.Name)
	assert.Equal(t, "epl_manchester_united.png", team.Logo)

	// "Man City" should resolve via alias map -> "Manchester City FC"
	team2 := handlers.TeamByLabel(teamsByName, "epl", "Man City", rawEvent)
	assert.NotNil(t, team2, "Expected to find team for Man City via alias")
	assert.Equal(t, "Manchester City FC", team2.Name)

	// "Arsenal" should resolve via soccer suffix variation -> "Arsenal FC"
	team3 := handlers.TeamByLabel(teamsByName, "epl", "Arsenal", rawEvent)
	assert.NotNil(t, team3, "Expected to find team for Arsenal via FC suffix")
	assert.Equal(t, "Arsenal FC", team3.Name)
}

func TestUCLTeamLogoResolution(t *testing.T) {
	// UCL event: API sends "Arsenal", "Barcelona", "Bayern Munich"
	// DB stores "Arsenal FC", "FC Barcelona", "FC Bayern München" with UCL-specific logos
	rawEvent := handlers.RawGammaEvent{
		ID:   "ucl-winner",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "champions-league"}},
		Markets: []handlers.RawGammaMarket{
			{GroupItemTitle: "Arsenal", OutcomePricesRaw: `["0.20"]`},
			{GroupItemTitle: "Barcelona", OutcomePricesRaw: `["0.12"]`},
			{GroupItemTitle: "Bayern Munich", OutcomePricesRaw: `["0.17"]`},
		},
	}

	// In the new system, QueryTeamsResolved populates the map with resolved aliases
	teamsByName := map[string]db.PlyMktTeam{
		"arsenal":                        {Name: "Arsenal FC", League: "ucl", Logo: "ucl_ars.png", Abbreviation: "ars"},
		"champions-league|arsenal":       {Name: "Arsenal FC", League: "ucl", Logo: "ucl_ars.png", Abbreviation: "ars"},
		"barcelona":                      {Name: "FC Barcelona", League: "ucl", Logo: "ucl_fcb.png", Abbreviation: "fcb"},
		"champions-league|barcelona":     {Name: "FC Barcelona", League: "ucl", Logo: "ucl_fcb.png", Abbreviation: "fcb"},
		"bayern munich":                  {Name: "FC Bayern München", League: "ucl", Logo: "ucl_bay.png", Abbreviation: "bay"},
		"champions-league|bayern munich": {Name: "FC Bayern München", League: "ucl", Logo: "ucl_bay.png", Abbreviation: "bay"},
	}

	// "Arsenal" should resolve via alias to "Arsenal FC"
	team := handlers.TeamByLabel(teamsByName, "champions-league", "Arsenal", rawEvent)
	assert.NotNil(t, team, "Expected to find Arsenal FC via alias resolution")
	assert.Equal(t, "Arsenal FC", team.Name)
	assert.Equal(t, "ucl_ars.png", team.Logo)

	// "Barcelona" should resolve via alias to "FC Barcelona"
	team2 := handlers.TeamByLabel(teamsByName, "champions-league", "Barcelona", rawEvent)
	assert.NotNil(t, team2, "Expected to find FC Barcelona via alias resolution")
	assert.Equal(t, "FC Barcelona", team2.Name)
	assert.Equal(t, "ucl_fcb.png", team2.Logo)

	// "Bayern Munich" should find "FC Bayern München" via alias map
	team3 := handlers.TeamByLabel(teamsByName, "champions-league", "Bayern Munich", rawEvent)
	assert.NotNil(t, team3, "Expected to find FC Bayern München via alias")
	assert.Equal(t, "FC Bayern München", team3.Name)
	assert.Equal(t, "ucl_bay.png", team3.Logo)
}

func TestArgentinaFIFATeamLookup(t *testing.T) {
	// FIFA World Cup: API sends "Argentina", DB has team with league "fif" and valid logo
	rawEvent := handlers.RawGammaEvent{
		ID:   "fifa-world-cup",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "fifa"}},
		Markets: []handlers.RawGammaMarket{
			{GroupItemTitle: "France", OutcomePricesRaw: `["0.12"]`},
			{GroupItemTitle: "Argentina", OutcomePricesRaw: `["0.10"]`},
			{GroupItemTitle: "Brazil", OutcomePricesRaw: `["0.09"]`},
		},
	}

	leaguesBySlug := map[string]db.League{
		"fifa": {Sport: "fif", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"france":        {Name: "France", League: "fif", Logo: "country-flags/fra.png", Abbreviation: "fra"},
		"fifa|france":   {Name: "France", League: "fif", Logo: "country-flags/fra.png", Abbreviation: "fra"},
		"argentina":     {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "arg"},
		"fifa|argentina": {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "arg"},
		"brazil":        {Name: "Brazil", League: "fif", Logo: "country-flags/bra.png", Abbreviation: "bra"},
		"fifa|brazil":   {Name: "Brazil", League: "fif", Logo: "country-flags/bra.png", Abbreviation: "bra"},
	}

	// Argentina should resolve directly via league-qualified or plain key
	team := handlers.TeamByLabel(teamsByName, "fifa", "Argentina", rawEvent)
	assert.NotNil(t, team, "Expected to find Argentina team")
	assert.Equal(t, "Argentina", team.Name)
	assert.Equal(t, "country-flags/arg.png", team.Logo)

	// Full enrichment should produce correct logos
	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)
	assert.NotNil(t, result)
	assert.Equal(t, "tournament", result.Type)

	// Find Argentina in participants
	found := false
	for _, p := range result.Participants {
		if p.Name == "Argentina" {
			found = true
			assert.Equal(t, "country-flags/arg.png", p.ImageURL)
		}
	}
	assert.True(t, found, "Expected Argentina in participants with country flag")
}

func TestEventLeagueSlugSkipsGenericTags(t *testing.T) {
	// EventLeagueSlug should prefer specific league tags over "sports"/"soccer"
	leaguesBySlug := map[string]db.League{
		"sports":           {Sport: "sports", Ordering: "home"},
		"soccer":           {Sport: "soccer", Ordering: "home"},
		"champions-league": {Sport: "ucl", Ordering: "home"},
	}

	rawEvent := handlers.RawGammaEvent{
		ID:   "ucl-event",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "soccer"}, {Slug: "champions-league"}},
	}

	slug := handlers.EventLeagueSlug(rawEvent, leaguesBySlug)
	assert.Equal(t, "champions-league", slug, "Expected specific league tag, not generic 'sports' or 'soccer'")
}

func TestSoccerMoneylineVersusEvent(t *testing.T) {
	// Soccer matchups use per-team Yes/No markets, not multi-outcome markets.
	// "Sport Lisboa e Benfica vs. Real Madrid CF" has 3 markets: Benfica Yes/No, Draw Yes/No, Real Madrid Yes/No
	rawEvent := handlers.RawGammaEvent{
		ID:     "ucl-match-1",
		Title:  "Sport Lisboa e Benfica vs. Real Madrid CF",
		GameID: handlers.FlexString("90107668"),
		Tags:   []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "ucl"}, {Slug: "soccer"}, {Slug: "games"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID: "m1", GroupItemTitle: "Sport Lisboa e Benfica", SportsMarketType: "moneyline",
				OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.25", "0.75"]`,
				Image: "champions-league-pic.png",
			},
			{
				ID: "m2", GroupItemTitle: "Draw (Sport Lisboa e Benfica vs. Real Madrid CF)", SportsMarketType: "moneyline",
				OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.28", "0.72"]`,
				Image: "champions-league-pic.png",
			},
			{
				ID: "m3", GroupItemTitle: "Real Madrid CF", SportsMarketType: "moneyline",
				OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.47", "0.53"]`,
				Image: "champions-league-pic.png",
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"ucl": {Sport: "ucl", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"sport lisboa e benfica":     {Name: "Sport Lisboa e Benfica", League: "ucl", Logo: "ucl_ben.png", Abbreviation: "ben"},
		"ucl|sport lisboa e benfica": {Name: "Sport Lisboa e Benfica", League: "ucl", Logo: "ucl_ben.png", Abbreviation: "ben"},
		"real madrid cf":             {Name: "Real Madrid CF", League: "ucl", Logo: "ucl_rma.png", Abbreviation: "rma"},
		"ucl|real madrid cf":         {Name: "Real Madrid CF", League: "ucl", Logo: "ucl_rma.png", Abbreviation: "rma"},
	}

	// EnrichSportsEvent should extract team names from GroupItemTitles (not "Yes"/"No")
	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)
	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Home team: Benfica with UCL logo
	assert.Equal(t, "Sport Lisboa e Benfica", result.Participants[0].Name)
	assert.Equal(t, "ucl_ben.png", result.Participants[0].ImageURL)
	assert.Equal(t, "home", result.Participants[0].Role)

	// Away team: Real Madrid with UCL logo
	assert.Equal(t, "Real Madrid CF", result.Participants[1].Name)
	assert.Equal(t, "ucl_rma.png", result.Participants[1].ImageURL)
	assert.Equal(t, "away", result.Participants[1].Role)

	// TransformEventV2 outcomes should also use team names
	v2Event := handlers.TransformEventV2(rawEvent, teamsByName, leaguesBySlug)
	assert.NotNil(t, v2Event)

	// Outcomes should be 3 (home, draw, away) with proper labels
	assert.Len(t, v2Event.Outcomes, 3)
	assert.Equal(t, "Sport Lisboa e Benfica", v2Event.Outcomes[0].Label)
	assert.Equal(t, "ucl_ben.png", v2Event.Outcomes[0].Image)
	assert.Equal(t, 0.25, v2Event.Outcomes[0].Probability)
	assert.Equal(t, "Real Madrid CF", v2Event.Outcomes[2].Label)
	assert.Equal(t, "ucl_rma.png", v2Event.Outcomes[2].Image)
}

func TestSoccerGroupEventEnrichment(t *testing.T) {
	// Full integration: UCL Winner event with all the name mismatches
	rawEvent := handlers.RawGammaEvent{
		ID:   "ucl-winner",
		Tags: []handlers.RawGammaTag{{Slug: "sports"}, {Slug: "champions-league"}},
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "Arsenal", OutcomePricesRaw: `["0.20"]`, Image: "generic-ucl.png"},
			{ID: "m2", GroupItemTitle: "Bayern Munich", OutcomePricesRaw: `["0.17"]`, Image: "generic-ucl.png"},
			{ID: "m3", GroupItemTitle: "Barcelona", OutcomePricesRaw: `["0.12"]`, Image: "generic-ucl.png"},
		},
		Image: "generic-ucl.png",
	}

	leaguesBySlug := map[string]db.League{
		"champions-league": {Sport: "ucl", Ordering: "home"},
	}

	// In the new system, QueryTeamsResolved populates the map with resolved aliases
	teamsByName := map[string]db.PlyMktTeam{
		"arsenal":                        {Name: "Arsenal FC", League: "ucl", Logo: "ucl_ars.png"},
		"champions-league|arsenal":       {Name: "Arsenal FC", League: "ucl", Logo: "ucl_ars.png"},
		"barcelona":                      {Name: "FC Barcelona", League: "ucl", Logo: "ucl_fcb.png"},
		"champions-league|barcelona":     {Name: "FC Barcelona", League: "ucl", Logo: "ucl_fcb.png"},
		"bayern munich":                  {Name: "FC Bayern München", League: "ucl", Logo: "ucl_bay.png"},
		"champions-league|bayern munich": {Name: "FC Bayern München", League: "ucl", Logo: "ucl_bay.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)
	assert.NotNil(t, result)
	assert.Equal(t, "tournament", result.Type)
	assert.Len(t, result.Participants, 3)

	// Arsenal (0.20) should be first, with UCL logo
	assert.Equal(t, "Arsenal", result.Participants[0].Name)
	assert.Equal(t, "ucl_ars.png", result.Participants[0].ImageURL)

	// Bayern Munich (0.17) should be second, with UCL logo
	assert.Equal(t, "Bayern Munich", result.Participants[1].Name)
	assert.Equal(t, "ucl_bay.png", result.Participants[1].ImageURL)

	// Barcelona (0.12) should be third, with UCL logo
	assert.Equal(t, "Barcelona", result.Participants[2].Name)
	assert.Equal(t, "ucl_fcb.png", result.Participants[2].ImageURL)
}

// =============================================================================
// SPORTS IMAGE CASCADE TESTS (PER-MARKET IMAGES AS FALLBACK)
// =============================================================================

func TestSportsGroupPerMarketImageFallback(t *testing.T) {
	// Futures event with 3 markets: market A has DB team with logo, market B has no DB team but has market.Image,
	// market C has no DB team and market.Image = "" (empty). Assert correct cascade behavior.
	rawEvent := handlers.RawGammaEvent{
		ID:       "nba-champions",
		Title:    "NBA Champions 2024-25",
		Category: "Sports",
		Tags:     []handlers.RawGammaTag{{Slug: "nba"}},
		Markets: []handlers.RawGammaMarket{
			{ID: "m1", GroupItemTitle: "Lakers", OutcomePricesRaw: `["0.20"]`, Image: "lakers-market.png"},
			{ID: "m2", GroupItemTitle: "Clippers", OutcomePricesRaw: `["0.15"]`, Image: "clippers-market.png"},
			{ID: "m3", GroupItemTitle: "Unknown Team", OutcomePricesRaw: `["0.05"]`, Image: ""}, // No market image
		},
		Image: "nba-generic.png", // Event image
	}

	leaguesBySlug := map[string]db.League{
		"nba": {Sport: "nba"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"lakers": {Name: "Lakers", League: "nba", Logo: "lakers-db-logo.png"}, // DB has logo
		// Clippers not in DB, should fall back to market image
		// Unknown Team not in DB and no market image, should fall back to empty/default
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "tournament", result.Type)
	assert.Len(t, result.Participants, 3)

	// Lakers: DB logo takes priority
	assert.Equal(t, "Lakers", result.Participants[0].Name)
	assert.Equal(t, "lakers-db-logo.png", result.Participants[0].ImageURL)

	// Clippers: No DB logo, falls back to market image
	assert.Equal(t, "Clippers", result.Participants[1].Name)
	assert.Equal(t, "clippers-market.png", result.Participants[1].ImageURL)

	// Unknown Team: No DB logo, no market image, should be empty/default
	assert.Equal(t, "Unknown Team", result.Participants[2].Name)
	assert.Equal(t, "", result.Participants[2].ImageURL) // Or some default, but not event image
}

func TestSoccerMoneylinePerMarketImages(t *testing.T) {
	// Soccer match with 3 per-team Yes/No markets (Benfica/benfica.png, Draw/draw.png, Real Madrid/realmadrid.png);
	// Benfica and Real Madrid resolve from DB (get DB logos), Draw has no DB team so gets its own market image.
	rawEvent := handlers.RawGammaEvent{
		ID:     "ucl-match",
		Title:  "Benfica vs Real Madrid",
		GameID: handlers.FlexString("90107668"),
		Tags:   []handlers.RawGammaTag{{Slug: "ucl"}, {Slug: "soccer"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID: "benfica-market", GroupItemTitle: "Benfica", SportsMarketType: "moneyline",
				OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.25", "0.75"]`,
				Image: "benfica-market.png",
			},
			{
				ID: "draw-market", GroupItemTitle: "Draw", SportsMarketType: "moneyline",
				OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.30", "0.70"]`,
				Image: "draw-market.png",
			},
			{
				ID: "realmadrid-market", GroupItemTitle: "Real Madrid", SportsMarketType: "moneyline",
				OutcomesRaw: `["Yes", "No"]`, OutcomePricesRaw: `["0.45", "0.55"]`,
				Image: "realmadrid-market.png",
			},
		},
		Image: "ucl-generic.png", // Event image
	}

	leaguesBySlug := map[string]db.League{
		"ucl": {Sport: "ucl", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"benfica":     {Name: "Benfica", League: "ucl", Logo: "benfica-db-logo.png"},
		"real madrid": {Name: "Real Madrid", League: "ucl", Logo: "realmadrid-db-logo.png"},
		// Draw not in DB, should fall back to market image
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2) // Draw excluded from versus

	// Home team: Benfica gets DB logo (not market image)
	assert.Equal(t, "Benfica", result.Participants[0].Name)
	assert.Equal(t, "benfica-db-logo.png", result.Participants[0].ImageURL)

	// Away team: Real Madrid gets DB logo (not market image)
	assert.Equal(t, "Real Madrid", result.Participants[1].Name)
	assert.Equal(t, "realmadrid-db-logo.png", result.Participants[1].ImageURL)
}

// =============================================================================
// ESPORTS EDGE CASES
// =============================================================================

func TestEsportsShortTeamNames(t *testing.T) {
	// Match with "T1" vs "Gen.G" (2-char and period-containing names);
	// verify they resolve from DB if present, else fall back to per-market images.
	rawEvent := handlers.RawGammaEvent{
		ID:       "lol-match",
		Title:    "T1 vs Gen.G",
		Category: "Sports",
		GameID:   handlers.FlexString("hltv123"),
		Tags:     []handlers.RawGammaTag{{Slug: "lol"}, {Slug: "esports"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "t1-market",
				OutcomesRaw:      `["T1", "Gen.G"]`,
				OutcomePricesRaw: `["0.55", "0.45"]`,
				Image:            "lol-matchup.png",
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"lol": {Sport: "lol", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"t1":   {Name: "T1", League: "lol", Logo: "t1-logo.png"},
		"gen.g": {Name: "Gen.G", League: "lol", Logo: "geng-logo.png"},
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Both teams resolve from DB
	assert.Equal(t, "T1", result.Participants[0].Name)
	assert.Equal(t, "t1-logo.png", result.Participants[0].ImageURL)

	assert.Equal(t, "Gen.G", result.Participants[1].Name)
	assert.Equal(t, "geng-logo.png", result.Participants[1].ImageURL)
}

func TestEsportsFallbackToMarketImage(t *testing.T) {
	// eSports match where no DB team exists; each outcome gets its market-specific image (not a generic event image).
	rawEvent := handlers.RawGammaEvent{
		ID:       "cs2-match",
		Title:    "Team Unknown vs Mystery Squad",
		Category: "Sports",
		GameID:   handlers.FlexString("hltv456"),
		Tags:     []handlers.RawGammaTag{{Slug: "cs2"}, {Slug: "esports"}},
		Image:    "esports-generic.png", // Generic event image
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "unknown-market",
				OutcomesRaw:      `["Team Unknown", "Mystery Squad"]`,
				OutcomePricesRaw: `["0.50", "0.50"]`,
				Image:            "cs2-matchup.png", // Market-specific image
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"cs2": {Sport: "cs2", Ordering: "home"},
	}

	// No teams in DB
	teamsByName := map[string]db.PlyMktTeam{}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Both teams should fall back to market image, not generic event image
	assert.Equal(t, "Team Unknown", result.Participants[0].Name)
	assert.Equal(t, "cs2-matchup.png", result.Participants[0].ImageURL)

	assert.Equal(t, "Mystery Squad", result.Participants[1].Name)
	assert.Equal(t, "cs2-matchup.png", result.Participants[1].ImageURL)
}

// =============================================================================
// PRIMARY MARKET SELECTION (DISPLAYDATA)
// =============================================================================

func TestMoneylineSelectedOverHighLiquidityMarket(t *testing.T) {
	// Sports event where a spread market has 10x the liquidity of the moneyline;
	// DisplayData must still use moneyline for participants.
	rawEvent := handlers.RawGammaEvent{
		ID:       "mlb-game",
		Title:    "Yankees vs Red Sox",
		Category: "Sports",
		GameID:   handlers.FlexString("789012"),
		Tags:     []handlers.RawGammaTag{{Slug: "mlb"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID: "spread-market", SportsMarketType: "spread",
				Liquidity: handlers.FlexFloat(1000000), // Very high liquidity
				OutcomesRaw: `["Yankees -1.5", "Red Sox +1.5"]`, OutcomePricesRaw: `["0.52", "0.48"]`,
			},
			{
				ID: "moneyline-market", SportsMarketType: "moneyline",
				Liquidity: handlers.FlexFloat(100000), // Much lower liquidity
				OutcomesRaw: `["Yankees", "Red Sox"]`, OutcomePricesRaw: `["0.55", "0.45"]`,
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"mlb": {Sport: "mlb", Ordering: "away"}, // MLB uses away ordering
	}

	result := handlers.EnrichSportsEvent(rawEvent, map[string]db.PlyMktTeam{}, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// DisplayData should use moneyline team names, not spread names
	assert.Equal(t, "Yankees", result.Participants[0].Name)  // Away team (MLB ordering)
	assert.Equal(t, "Red Sox", result.Participants[1].Name)  // Home team
}

// =============================================================================
// CROSS-LEAGUE TESTS
// =============================================================================

func TestSameTeamNameDifferentLeagues(t *testing.T) {
	// "Team Liquid" in both LoL (`lol|team liquid`) and CS2 (`csgo|team liquid`) leagues with different logos;
	// an event tagged "cs2" must get the CS2 logo, not the LoL one.
	rawEvent := handlers.RawGammaEvent{
		ID:       "team-liquid-cs2-match",
		Title:    "Team Liquid vs FaZe Clan",
		Category: "Sports",
		GameID:   handlers.FlexString("hltv789"),
		Tags:     []handlers.RawGammaTag{{Slug: "cs2"}, {Slug: "esports"}},
		Markets: []handlers.RawGammaMarket{
			{
				ID:               "cs2-market",
				OutcomesRaw:      `["Team Liquid", "FaZe Clan"]`,
				OutcomePricesRaw: `["0.60", "0.40"]`,
			},
		},
	}

	leaguesBySlug := map[string]db.League{
		"cs2": {Sport: "cs2", Ordering: "home"},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"team liquid":         {Name: "Team Liquid", League: "lol", Logo: "liquid-lol-logo.png"},
		"lol|team liquid":     {Name: "Team Liquid", League: "lol", Logo: "liquid-lol-logo.png"},
		"cs2|team liquid":     {Name: "Team Liquid", League: "cs2", Logo: "liquid-cs2-logo.png"}, // Different logo
	}

	result := handlers.EnrichSportsEvent(rawEvent, teamsByName, leaguesBySlug)

	assert.NotNil(t, result)
	assert.Equal(t, "versus_match", result.Type)
	assert.Len(t, result.Participants, 2)

	// Should get CS2 logo because event is tagged "cs2"
	assert.Equal(t, "Team Liquid", result.Participants[0].Name)
	assert.Equal(t, "liquid-cs2-logo.png", result.Participants[0].ImageURL, "Should get CS2 logo, not LoL logo")
}
