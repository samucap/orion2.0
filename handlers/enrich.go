package handlers

import (
	"strings"

	"github.com/samucap/orion2.0/internal/db"
)

// =============================================================================
// TEAM IMAGE RESOLUTION (single source of truth for the logo cascade)
// =============================================================================

// ResolveTeamImage returns the best image URL for a team/participant label.
// Cascade: DB team logo → derived from same-league peers → raw API Teams[].Logo → derived from league template → market image (last resort).
// Market image is last because it's usually a generic event banner, not a team logo.
// eventImage is the event-level icon/image; if marketImage equals it, we treat it as generic and skip.
func ResolveTeamImage(label string, team *db.PlyMktTeam, raw RawGammaEvent, eventLeague string, leaguesBySlug map[string]db.League, marketImage, eventImage string, teamsByName map[string]db.PlyMktTeam) string {
	// 1. DB team logo
	if team != nil && team.Logo != "" {
		return team.Logo
	}
	// 1.5: If DB team found but no logo, derive from same-league peers
	if team != nil && team.Logo == "" && team.Abbreviation != "" {
		if pattern := InferLeagueLogoPattern(team.League, teamsByName); pattern != "" {
			return strings.Replace(pattern, "{abbrev}", strings.ToLower(team.Abbreviation), 1)
		}
	}
	// 2. Raw API Teams[].Logo (matched by name/abbreviation)
	if img := teamLogoFromRaw(raw, label); img != "" {
		return img
	}
	// 3. Derived from league template ({team} or {abbrev})
	if img := deriveTeamLogoURL(eventLeague, label, abbreviationForLabel(team, raw, label), leaguesBySlug); img != "" {
		return img
	}
	// 3.5: Country-flags safety net for international events
	// DB teams should have logos, but if lookup missed, derive from known country names
	if code := countryNameToCode[strings.ToLower(strings.TrimSpace(label))]; code != "" {
		return "https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/" + code + ".png"
	}
	// 4. Market image — only if it's team-specific (not the same generic event banner)
	if marketImage != "" && marketImage != eventImage {
		return marketImage
	}
	// 5. Fall through to market image even if generic (better than nothing)
	return marketImage
}

// ResolveTeamColor returns the team color from DB when available.
func ResolveTeamColor(team *db.PlyMktTeam) string {
	if team != nil {
		return team.Color
	}
	return ""
}

// =============================================================================
// LEAGUE + TEAM LOOKUP HELPERS
// =============================================================================

// genericSportsTags are tag slugs that represent broad categories, not specific leagues.
// EventLeagueSlug skips these when a more specific league tag is available.
var genericSportsTags = map[string]bool{
	"sports": true, "soccer": true, "esports": true, "football": true,
}

// EventLeagueSlug returns the event's league slug for team lookup and derived logos.
// Prefers a tag with a logo_url_template (specific league like "nba" over generic "sports").
// Skips generic tags ("sports", "soccer") when a more specific league tag exists.
func EventLeagueSlug(raw RawGammaEvent, leaguesBySlug map[string]db.League) string {
	var firstMatch string
	var firstSpecific string
	for _, tag := range raw.Tags {
		s := strings.ToLower(tag.Slug)
		if s == "" {
			continue
		}
		league, exists := leaguesBySlug[s]
		if !exists {
			continue
		}
		if firstMatch == "" {
			firstMatch = s
		}
		if firstSpecific == "" && !genericSportsTags[s] {
			firstSpecific = s
		}
		if league.LogoURLTemplate != "" {
			return s
		}
	}
	if firstSpecific != "" {
		return firstSpecific
	}
	return firstMatch
}

// soccerSuffixes and soccerPrefixes are common name parts that DB stores but the API omits.
var soccerSuffixes = []string{" fc", " cf", " sk", " sc", " kv", " bc", " 1909"}
var soccerPrefixes = []string{"fc ", "afc ", "bsc ", "as ", "ss ", "ssc "}

// TeamByLabel looks up a team by label (case-insensitive, UTF-8-safe).
// Uses a two-phase approach: Phase 1 tries all strategies with the league qualifier
// (preventing cross-league mismatches), Phase 2 tries plain keys as fallback.
func TeamByLabel(teamsByName map[string]db.PlyMktTeam, eventLeague, label string, raw RawGammaEvent) *db.PlyMktTeam {
	key := db.NormalizeTeamKey(label)
	if key == "" {
		return nil
	}
	lk := ""
	if eventLeague != "" {
		lk = db.NormalizeTeamKey(eventLeague)
	}

	words := strings.Fields(label)

	// ── Phase 1: League-qualified lookups (all strategies with league prefix) ──
	// This ensures the correct league's team is found before any cross-league match.
	if lk != "" {
		// 1a. Direct league-qualified key
		if t := lookupLeague(teamsByName, lk, key); t != nil {
			return t
		}
		// 1b. Alias map with league qualifier
		if canonical, ok := teamLabelAliases[key]; ok {
			if cKey := db.NormalizeTeamKey(canonical); cKey != "" {
				if t := lookupLeague(teamsByName, lk, cKey); t != nil {
					return t
				}
			}
		}
		// 1c. Soccer name variations with league qualifier
		for _, sfx := range soccerSuffixes {
			if t := lookupLeague(teamsByName, lk, key+sfx); t != nil {
				return t
			}
		}
		for _, pfx := range soccerPrefixes {
			if t := lookupLeague(teamsByName, lk, pfx+key); t != nil {
				return t
			}
		}
		// 1d. Suffix stripping with league qualifier
		for i := 1; i < len(words); i++ {
			if sfxKey := db.NormalizeTeamKey(strings.Join(words[i:], " ")); sfxKey != "" {
				if t := lookupLeague(teamsByName, lk, sfxKey); t != nil {
					return t
				}
			}
		}
		// 1e. Abbreviation fallback with league qualifier
		lower := strings.ToLower(label)
		for _, rt := range raw.Teams {
			if strings.ToLower(rt.Name) == lower && rt.Abbreviation != "" {
				if aKey := db.NormalizeTeamKey(rt.Abbreviation); aKey != "" {
					if t := lookupLeague(teamsByName, lk, aKey); t != nil {
						return t
					}
				}
			}
		}
	}

	// ── Phase 2: Plain key lookups (cross-league fallback) ──
	if t := lookupPlain(teamsByName, key); t != nil {
		return t
	}
	if canonical, ok := teamLabelAliases[key]; ok {
		if cKey := db.NormalizeTeamKey(canonical); cKey != "" {
			if t := lookupPlain(teamsByName, cKey); t != nil {
				return t
			}
		}
	}
	for _, sfx := range soccerSuffixes {
		if t := lookupPlain(teamsByName, key+sfx); t != nil {
			return t
		}
	}
	for _, pfx := range soccerPrefixes {
		if t := lookupPlain(teamsByName, pfx+key); t != nil {
			return t
		}
	}
	for i := 1; i < len(words); i++ {
		if sfxKey := db.NormalizeTeamKey(strings.Join(words[i:], " ")); sfxKey != "" {
			if t := lookupPlain(teamsByName, sfxKey); t != nil {
				return t
			}
		}
	}
	lower := strings.ToLower(label)
	for _, rt := range raw.Teams {
		if strings.ToLower(rt.Name) == lower && rt.Abbreviation != "" {
			if aKey := db.NormalizeTeamKey(rt.Abbreviation); aKey != "" {
				if t := lookupPlain(teamsByName, aKey); t != nil {
					return t
				}
			}
		}
	}
	return nil
}

// lookupLeague tries a league-qualified key only.
func lookupLeague(teamsByName map[string]db.PlyMktTeam, leagueKey, key string) *db.PlyMktTeam {
	if team, ok := teamsByName[leagueKey+"|"+key]; ok {
		return &team
	}
	return nil
}

// lookupPlain tries a plain (cross-league) key only.
func lookupPlain(teamsByName map[string]db.PlyMktTeam, key string) *db.PlyMktTeam {
	if team, ok := teamsByName[key]; ok {
		return &team
	}
	return nil
}

// =============================================================================
// INTERNAL HELPERS (unexported)
// =============================================================================

func normalizeLabelToSlug(label string) string {
	s := strings.TrimSpace(label)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

func abbreviationForLabel(team *db.PlyMktTeam, raw RawGammaEvent, label string) string {
	if team != nil && team.Abbreviation != "" {
		return strings.TrimSpace(team.Abbreviation)
	}
	lower := strings.ToLower(label)
	for _, t := range raw.Teams {
		if strings.ToLower(t.Name) == lower || strings.ToLower(t.Abbreviation) == lower {
			return strings.TrimSpace(t.Abbreviation)
		}
	}
	return ""
}

// teamLabelAliases maps common shortened/alternative team names used by the Polymarket API
// to the canonical DB name so TeamByLabel can resolve them. Keys must be lowercase.
var teamLabelAliases = map[string]string{
	// EPL short names -> DB canonical names
	"man united":    "manchester united fc",
	"man utd":       "manchester united fc",
	"man city":      "manchester city fc",
	"aston villa":   "aston villa fc",
	"spurs":         "tottenham hotspur fc",
	"wolves":        "wolverhampton wanderers fc",
	"west ham":      "west ham united fc",
	"newcastle":     "newcastle united fc",
	"crystal palace": "crystal palace fc",
	"nott'm forest": "nottingham forest fc",
	"nottm forest":  "nottingham forest fc",
	"bournemouth":   "afc bournemouth",

	// UCL / European club short names -> DB canonical names
	"barcelona":          "fc barcelona",
	"barca":              "fc barcelona",
	"barça":              "fc barcelona",
	"bayern munich":      "fc bayern münchen",
	"bayern":             "fc bayern münchen",
	"bayern münchen":     "fc bayern münchen",
	"real madrid":        "real madrid cf",
	"juventus":           "juventus fc",
	"juve":               "juventus fc",
	"psg":                "paris saint-germain fc",
	"paris saint-germain": "paris saint-germain fc",
	"inter milan":        "fc internazionale milano",
	"inter":              "fc internazionale milano",
	"internazionale":     "fc internazionale milano",
	"atletico madrid":    "club atlético de madrid",
	"atletico":           "club atlético de madrid",
	"atlético madrid":    "club atlético de madrid",
	"atlético":           "club atlético de madrid",
	"dortmund":           "bv borussia 09 dortmund",
	"borussia dortmund":  "bv borussia 09 dortmund",
	"galatasaray":        "galatasaray sk",
	"benfica":            "sport lisboa e benfica",
	"bayer leverkusen":   "bayer 04 leverkusen",
	"leverkusen":         "bayer 04 leverkusen",
	"porto":              "fc porto",
	"monaco":             "as monaco fc",
	"salzburg":           "fc red bull salzburg",
	"red bull salzburg":  "fc red bull salzburg",
	"ajax":               "afc ajax",
	"roma":               "as roma",
	"atalanta":           "atalanta bc",
	"young boys":         "bsc young boys",
	"club brugge":        "club brugge kv",
	"celtic":             "celtic fc",
	"feyenoord":          "feyenoord rotterdam",
	"psv":                "psv eindhoven",
	"sporting":           "sporting clube de portugal",
	"sporting cp":        "sporting clube de portugal",
	"napoli":             "ssc napoli",
	"lazio":              "ss lazio",
	"sevilla":            "sevilla fc",
	"villarreal":         "villarreal cf",
	"marseille":          "olympique de marseille",
	"lyon":               "olympique lyonnais",
	"lille":              "losc lille",
	"leipzig":            "rb leipzig",
	"rb leipzig":         "rasenballsport leipzig",
	"stuttgart":          "vfb stuttgart",
	"wolfsburg":          "vfl wolfsburg",
	"bruges":             "club brugge kv",
	"girona":             "girona fc",
	"bologna":            "bologna fc 1909",
	"slovan bratislava":  "šk slovan bratislava",
	"red star belgrade":  "fk crvena zvezda",
	"dinamo zagreb":      "gnk dinamo zagreb",
	"shakhtar donetsk":   "fc shakhtar donetsk",
	"shakhtar":           "fc shakhtar donetsk",
}

// countryNameToCode: verified Polymarket S3 country-flags abbreviations (via scripts/verify_country_flags/).
var countryNameToCode = map[string]string{
	"afghanistan": "afg", "albania": "alb", "algeria": "dz", "argentina": "arg", "australia": "aus",
	"austria": "at", "belgium": "be", "bolivia": "bol", "bosnia and herzegovina": "bih", "brazil": "bra",
	"bulgaria": "bg", "cameroon": "cmr", "canada": "can", "chile": "cl", "china": "cn",
	"colombia": "co", "costa rica": "cr", "croatia": "hr", "czech republic": "cze", "denmark": "dk",
	"ecuador": "ecu", "egypt": "egy", "england": "eng", "finland": "fin", "france": "fra",
	"germany": "deu", "ghana": "gha", "greece": "gr", "hungary": "hun", "iceland": "is",
	"iran": "irn", "ireland": "ie", "italy": "ita", "ivory coast": "civ", "jamaica": "jm",
	"japan": "jpn", "mexico": "mex", "morocco": "mar", "netherlands": "nl", "new zealand": "nz",
	"nigeria": "nga", "north korea": "prk", "norway": "no", "paraguay": "py", "peru": "per",
	"poland": "pol", "portugal": "pt", "republic of ireland": "irl", "romania": "rou", "russia": "ru",
	"saudi arabia": "sau", "scotland": "sco", "senegal": "sn", "serbia": "rs", "slovakia": "sk",
	"slovenia": "svn", "south africa": "za", "south korea": "kor", "spain": "esp", "sweden": "swe",
	"switzerland": "che", "tunisia": "tun", "turkey": "tr", "ukraine": "ukr",
	"united arab emirates": "ae", "united states": "usa", "usa": "usa", "us": "usa",
	"uruguay": "uy", "venezuela": "ven", "wales": "wal",
}

func deriveTeamLogoURL(leagueSlug, teamLabel, abbrev string, leaguesBySlug map[string]db.League) string {
	if leagueSlug == "" || teamLabel == "" {
		return ""
	}
	league, exists := leaguesBySlug[leagueSlug]
	if !exists || league.LogoURLTemplate == "" {
		return ""
	}
	tpl := league.LogoURLTemplate
	if strings.Contains(tpl, "{abbrev}") {
		if abbrev == "" && strings.Contains(tpl, "country-flags") {
			abbrev = countryNameToCode[strings.ToLower(strings.TrimSpace(teamLabel))]
		}
		if abbrev == "" {
			return ""
		}
		abbrev = strings.ToLower(strings.TrimSpace(abbrev))
	}
	slug := normalizeLabelToSlug(teamLabel)
	if slug == "" {
		return ""
	}
	return strings.NewReplacer("{team}", slug, "{abbrev}", abbrev).Replace(tpl)
}

func teamLogoFromRaw(raw RawGammaEvent, name string) string {
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	for _, t := range raw.Teams {
		if t.Logo != "" && (strings.ToLower(t.Name) == lower || strings.ToLower(t.Abbreviation) == lower) {
			return t.Logo
		}
	}
	return ""
}

// InferLeagueLogoPattern analyzes teams from the same league that have logos
// to find a common URL pattern and returns it with {abbrev} placeholder.
func InferLeagueLogoPattern(league string, teamsByName map[string]db.PlyMktTeam) string {
	if league == "" {
		return ""
	}

	var sampleLogos []string
	leagueLower := strings.ToLower(league)

	// Collect up to 5 sample logos from teams in the same league
	for _, team := range teamsByName {
		if strings.ToLower(team.League) == leagueLower && team.Logo != "" && team.Abbreviation != "" {
			sampleLogos = append(sampleLogos, team.Logo)
			if len(sampleLogos) >= 5 {
				break
			}
		}
	}

	if len(sampleLogos) < 2 {
		return "" // Need at least 2 samples to establish a pattern
	}

	// Find common prefix/suffix pattern
	// For example: ["NHL+Team+Logos/COL.png", "NHL+Team+Logos/TB.png"] -> "NHL+Team+Logos/{abbrev}.png"
	commonPrefix := sampleLogos[0]
	commonSuffix := sampleLogos[0]

	// Find longest common prefix
	for _, logo := range sampleLogos[1:] {
		for len(commonPrefix) > 0 && !strings.HasPrefix(logo, commonPrefix) {
			commonPrefix = commonPrefix[:len(commonPrefix)-1]
		}
	}

	// Find longest common suffix (excluding file extension)
	for _, logo := range sampleLogos[1:] {
		for len(commonSuffix) > 0 && !strings.HasSuffix(logo, commonSuffix) {
			commonSuffix = commonSuffix[1:]
		}
	}

	// If we have a meaningful common prefix and suffix, create pattern
	if len(commonPrefix) > 10 && len(commonSuffix) > 4 { // reasonable minimum lengths
		// Extract the part between prefix and suffix as the abbreviation position
		remaining := sampleLogos[0][len(commonPrefix) : len(sampleLogos[0])-len(commonSuffix)]
		if len(remaining) <= 5 { // abbreviation should be short
			return commonPrefix + "{abbrev}" + commonSuffix
		}
	}

	return ""
}

// eventImage returns the event-level icon (or image) so callers can detect generic market banners.
func eventImage(raw RawGammaEvent) string {
	if raw.Icon != "" {
		return raw.Icon
	}
	return raw.Image
}

// =============================================================================
// DISPLAY DATA BUILDERS
// =============================================================================

// EnrichSportsEvent builds the DisplayData (versus or tournament) for a sports event.
func EnrichSportsEvent(
	raw RawGammaEvent,
	teamsByName map[string]db.PlyMktTeam,
	leaguesBySlug map[string]db.League,
) *V2DisplayData {
	// Find league metadata, preferring specific league tags over generic ones ("sports", "soccer").
	var league *db.League
	var genericLeague *db.League
	for _, tag := range raw.Tags {
		s := strings.ToLower(tag.Slug)
		if l, exists := leaguesBySlug[s]; exists {
			if genericSportsTags[s] {
				if genericLeague == nil {
					genericLeague = &l
				}
				continue
			}
			league = &l
			break
		}
	}
	if league == nil {
		league = genericLeague
	}
	if isSportsGroupEvent(raw) {
		return buildSportsGroupDisplayData(raw, teamsByName, league, leaguesBySlug)
	}
	return buildSportsVersusDisplayData(raw, teamsByName, league, leaguesBySlug)
}

func buildSportsVersusDisplayData(
	raw RawGammaEvent,
	teamsByName map[string]db.PlyMktTeam,
	league *db.League,
	leaguesBySlug map[string]db.League,
) *V2DisplayData {
	mainMarket := highestLiquidityMarket(raw)
	if mainMarket == nil {
		return nil
	}
	outcomes := parseRawOutcomes(mainMarket.OutcomesRaw)
	if len(outcomes) < 2 {
		return nil
	}

	ordering := "home"
	if league != nil {
		ordering = league.Ordering
	}

	eventLeague := EventLeagueSlug(raw, leaguesBySlug)
	evImg := eventImage(raw)

	// Soccer moneyline detection: if outcomes are Yes/No, extract team names
	// from GroupItemTitles instead (soccer has per-team markets, not multi-outcome).
	if isYesNoOutcomes(outcomes) {
		if teamNames := extractVersusTeamNames(raw); len(teamNames) >= 2 {
			outcomes = teamNames
		}
	}

	// Build participants for the two outcomes
	participants := make([]V2Participant, 0, 2)
	roles := [2]string{"home", "away"}
	if ordering == "away" {
		roles = [2]string{"away", "home"}
	}

	for i := 0; i < 2; i++ {
		mktImg := marketImageForLabel(raw, outcomes[i])
		if mktImg == "" {
			mktImg = mainMarket.Image
		}
		team := TeamByLabel(teamsByName, eventLeague, outcomes[i], raw)
		participants = append(participants, V2Participant{
			Name:     outcomes[i],
			Role:     roles[i],
			Color:    ResolveTeamColor(team),
			ImageURL: ResolveTeamImage(outcomes[i], team, raw, eventLeague, leaguesBySlug, mktImg, evImg, teamsByName),
		})
	}

	return &V2DisplayData{Type: "versus_match", Participants: participants}
}

// isYesNoOutcomes returns true if outcomes are just ["Yes","No"] (moneyline-per-team style).
func isYesNoOutcomes(outcomes []string) bool {
	if len(outcomes) < 2 {
		return false
	}
	a, b := strings.ToLower(outcomes[0]), strings.ToLower(outcomes[1])
	return (a == "yes" && b == "no") || (a == "no" && b == "yes")
}

// extractVersusTeamNames pulls team names from GroupItemTitles, filtering out
// Draw/O-U/spread markets. Returns the first two team names found (home, away order).
func extractVersusTeamNames(raw RawGammaEvent) []string {
	var names []string
	for _, m := range raw.Markets {
		git := strings.TrimSpace(m.GroupItemTitle)
		if git == "" {
			continue
		}
		lower := strings.ToLower(git)
		if strings.HasPrefix(lower, "draw") ||
			strings.HasPrefix(lower, "o/u") ||
			strings.HasPrefix(lower, "over") ||
			strings.HasPrefix(lower, "under") ||
			strings.Contains(lower, "(-") ||
			strings.Contains(lower, "(+") {
			continue
		}
		names = append(names, git)
		if len(names) >= 2 {
			break
		}
	}
	return names
}

// marketImageForLabel finds the market image for a specific team label in moneyline events.
func marketImageForLabel(raw RawGammaEvent, label string) string {
	lower := strings.ToLower(label)
	for _, m := range raw.Markets {
		if strings.ToLower(strings.TrimSpace(m.GroupItemTitle)) == lower {
			return m.Image
		}
	}
	return ""
}

func buildSportsGroupDisplayData(
	raw RawGammaEvent,
	teamsByName map[string]db.PlyMktTeam,
	league *db.League,
	leaguesBySlug map[string]db.League,
) *V2DisplayData {
	eventLeague := EventLeagueSlug(raw, leaguesBySlug)
	evImg := eventImage(raw)
	marketPrices := sortedMarketPrices(raw)
	participants := make([]V2Participant, 0, 5)

	for i, mp := range marketPrices {
		if i >= 5 || mp.price == 0 {
			if mp.price == 0 {
				continue
			}
			break
		}
		label := mp.market.GroupItemTitle
		if label == "" {
			label = stripEventTitle(mp.market.Question, raw.Title)
		}
		team := TeamByLabel(teamsByName, eventLeague, label, raw)
		participants = append(participants, V2Participant{
			Name:     label,
			Role:     "player_1",
			Color:    ResolveTeamColor(team),
			ImageURL: ResolveTeamImage(label, team, raw, eventLeague, leaguesBySlug, mp.market.Image, evImg, teamsByName),
		})
	}
	return &V2DisplayData{Type: "tournament", Participants: participants}
}

// =============================================================================
// SHARED UTILITIES
// =============================================================================

func highestLiquidityMarket(raw RawGammaEvent) *RawGammaMarket {
	var best *RawGammaMarket
	max := 0.0
	for i := range raw.Markets {
		liq := raw.Markets[i].Liquidity.Float64()
		if liq > max {
			max = liq
			best = &raw.Markets[i]
		}
	}
	if best == nil && len(raw.Markets) > 0 {
		best = &raw.Markets[0]
	}
	return best
}

type marketWithPrice struct {
	market RawGammaMarket
	price  float64
}

func sortedMarketPrices(raw RawGammaEvent) []marketWithPrice {
	out := make([]marketWithPrice, 0, len(raw.Markets))
	for _, m := range raw.Markets {
		prices := parseRawPrices(m.OutcomePricesRaw)
		p := 0.0
		if len(prices) > 0 {
			p = prices[0]
		}
		out = append(out, marketWithPrice{m, p})
	}
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].price > out[i].price {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
