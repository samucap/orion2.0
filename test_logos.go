package main

import (
	"fmt"

	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
)

func main() {
	fmt.Println("🧪 Testing Team Logo Resolution...")

	// Test 1: Abbreviation fallback (NHL)
	fmt.Println("\n1. Testing NHL abbreviation fallback (Colorado Avalanche -> COL)")
	testAbbreviationFallback()

	// Test 2: Direct DB lookup (NBA)
	fmt.Println("\n2. Testing direct DB lookup (Los Angeles Lakers)")
	testDirectLookup()

	// Test 3: FIFA tag mapping
	fmt.Println("\n3. Testing FIFA tag mapping (fifa -> fif)")
	testFifaMapping()

	fmt.Println("\n✅ Logo resolution tests completed!")
}

func testAbbreviationFallback() {
	// Simulate NHL event with full team names
	rawEvent := handlers.RawGammaEvent{
		ID:   "nhl-test",
		Tags: []handlers.RawGammaTag{{Slug: "nhl"}},
		Teams: []handlers.RawGammaTeam{
			{Name: "Colorado Avalanche", Abbreviation: "COL"},
		},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Colorado Avalanche", "Dallas Stars"]`},
		},
	}

	// Mock teams DB (short names like actual DB)
	teamsByName := map[string]db.PlyMktTeam{
		"avalanche":     {Name: "Avalanche", League: "nhl", Logo: "NHL+Team+Logos/COL.png", Abbreviation: "COL"},
		"nhl|avalanche": {Name: "Avalanche", League: "nhl", Logo: "NHL+Team+Logos/COL.png", Abbreviation: "COL"},
		"col":           {Name: "Avalanche", League: "nhl", Logo: "NHL+Team+Logos/COL.png", Abbreviation: "COL"},
		"nhl|col":       {Name: "Avalanche", League: "nhl", Logo: "NHL+Team+Logos/COL.png", Abbreviation: "COL"},
	}

	leaguesBySlug := map[string]db.League{
		"nhl": {Sport: "nhl", Ordering: "home"},
	}

	// Test team lookup
	team := handlers.TeamByLabel(teamsByName, "nhl", "Colorado Avalanche", rawEvent)
	if team != nil && team.Logo == "NHL+Team+Logos/COL.png" {
		fmt.Println("  ✅ Colorado Avalanche -> NHL+Team+Logos/COL.png")
	} else {
		fmt.Println("  ❌ Colorado Avalanche lookup failed")
	}

	// Test full resolution
	image := handlers.ResolveTeamImage("Colorado Avalanche", team, rawEvent, "nhl", leaguesBySlug, "", "", teamsByName)
	if image == "NHL+Team+Logos/COL.png" {
		fmt.Println("  ✅ Full resolution: Colorado Avalanche -> NHL+Team+Logos/COL.png")
	} else {
		fmt.Printf("  ❌ Full resolution failed: got '%s'\n", image)
	}
}

func testDirectLookup() {
	// Test direct DB lookup (names match exactly)
	rawEvent := handlers.RawGammaEvent{
		ID:   "nba-test",
		Tags: []handlers.RawGammaTag{{Slug: "nba"}},
		Teams: []handlers.RawGammaTeam{
			{Name: "Los Angeles Lakers", Abbreviation: "LAL"},
		},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Los Angeles Lakers", "Boston Celtics"]`},
		},
	}

	teamsByName := map[string]db.PlyMktTeam{
		"los angeles lakers":     {Name: "Los Angeles Lakers", League: "nba", Logo: "NBA+Team+Logos/LAL.png"},
		"nba|los angeles lakers": {Name: "Los Angeles Lakers", League: "nba", Logo: "NBA+Team+Logos/LAL.png"},
	}

	team := handlers.TeamByLabel(teamsByName, "nba", "Los Angeles Lakers", rawEvent)
	if team != nil && team.Logo == "NBA+Team+Logos/LAL.png" {
		fmt.Println("  ✅ Los Angeles Lakers -> NBA+Team+Logos/LAL.png")
	} else {
		fmt.Println("  ❌ Los Angeles Lakers lookup failed")
	}
}

func testFifaMapping() {
	// Test FIFA tag mapping (fifa tag should find fif league teams)
	rawEvent := handlers.RawGammaEvent{
		ID:   "fifa-test",
		Tags: []handlers.RawGammaTag{{Slug: "fifa"}},
		Teams: []handlers.RawGammaTeam{
			{Name: "Argentina", Abbreviation: "ARG"},
		},
		Markets: []handlers.RawGammaMarket{
			{OutcomesRaw: `["Argentina", "Brazil"]`},
		},
	}

	// Mock teams with fif league (DB) and fifa tag mapping
	teamsByName := map[string]db.PlyMktTeam{
		// DB teams in fif league
		"argentina":     {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "ARG"},
		"fif|argentina": {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "ARG"},
		"arg":           {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "ARG"},
		"fif|arg":       {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "ARG"},
		// Tag-slug composite keys (added by our team loading logic)
		"fifa|argentina": {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "ARG"},
		"fifa|arg":       {Name: "Argentina", League: "fif", Logo: "country-flags/arg.png", Abbreviation: "ARG"},
	}

	// Test lookup with fifa tag (should find via tag-slug composite key)
	team := handlers.TeamByLabel(teamsByName, "fifa", "Argentina", rawEvent)
	if team != nil && team.Logo == "country-flags/arg.png" {
		fmt.Println("  ✅ FIFA tag mapping: Argentina -> country-flags/arg.png")
	} else {
		fmt.Println("  ❌ FIFA tag mapping failed")
	}
}
