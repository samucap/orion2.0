// Polymarket S3 image URL exploration script.
// Fetches events from the Gamma API (different types/categories), checks which
// image URLs return 200, infers path patterns, then tries derived URLs
// (e.g. base + slug + ".png") to see if we can get 200s.
//
// With -discover-templates: fetches teams from Gamma GET /teams, discovers logo URL
// templates per league, HEAD-verifies them, and prints SQL (or runs -update-db to set leagues.logo_url_template).
//
// Run: go run github.com/samucap/orion2.0/scripts/polymarket_s3_explore
//
//	go run github.com/samucap/orion2.0/scripts/polymarket_s3_explore -discover-templates
//	go run github.com/samucap/orion2.0/scripts/polymarket_s3_explore -discover-templates -update-db
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
)

const (
	s3Base      = "https://polymarket-upload.s3.us-east-2.amazonaws.com/"
	gammaBase   = "https://gamma-api.polymarket.com"
	limit       = 60 // events per request
	headWorkers = 8
	maxHEAD     = 250 // cap HEAD requests per run
)

// logoSample is a single S3 logo path sample tied to a league tag and team name/abbrev.
type logoSample struct {
	tagSlug string
	folder  string
	stem    string
	name    string
	abbrev  string
}

func main() {
	discoverTemplates := flag.Bool("discover-templates", false, "discover logo URL templates per league from API, verify with HEAD, output SQL or update DB")
	updateDB := flag.Bool("update-db", false, "with -discover-templates, run UPDATE on leagues table (requires POSTGRES_* env)")
	flag.Parse()

	if *discoverTemplates {
		runDiscoverTemplates(*updateDB)
		return
	}

	runExplore()
}

func runExplore() {
	// Fetch events from several queries to get variety (mixed, politics, sports with relaxed filters)
	queries := []struct {
		name string
		path string
	}{
		{"default (mixed)", "/events?closed=false&active=true&archived=false&ascending=false&limit=" + fmt.Sprint(limit) + "&order=volume24hr&liquidity_min=500&volume_min=500&tag_id=100215&related_tags=true"},
		{"politics-ish tag (liquidity min)", "/events?closed=false&active=true&archived=false&ascending=false&limit=" + fmt.Sprint(limit) + "&order=volume24hr&liquidity_min=500&volume_min=500&tag_id=2"},
		{"sports (liquidity min)", "/events?closed=false&active=true&archived=false&ascending=false&limit=" + fmt.Sprint(limit) + "&order=volume24hr&liquidity_min=500&volume_min=500&tag_id=100639"},
		{"crypto (liquidity min)", "/events?closed=false&active=true&archived=false&ascending=false&limit=" + fmt.Sprint(limit) + "&order=volume24hr&liquidity_min=200&volume_min=200&tag_id=21"},
		{"more mixed (liquidity)", "/events?closed=false&active=true&archived=false&ascending=false&limit=" + fmt.Sprint(limit) + "&order=liquidity&liquidity_min=200&volume_min=200"},
	}

	var allEvents []handlers.RawGammaEvent
	seen := make(map[string]bool)
	for _, q := range queries {
		resp, err := http.Get(gammaBase + q.path)
		if err != nil {
			fmt.Printf("fetch %s: %v\n", q.name, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("fetch %s: status %d\n", q.name, resp.StatusCode)
			continue
		}
		var events []handlers.RawGammaEvent
		if err := json.Unmarshal(body, &events); err != nil {
			fmt.Printf("parse %s: %v\n", q.name, err)
			continue
		}
		for _, e := range events {
			if !seen[e.ID] {
				seen[e.ID] = true
				allEvents = append(allEvents, e)
			}
		}
		fmt.Printf("fetched %s: %d events (total unique: %d)\n", q.name, len(events), len(allEvents))
	}

	// Collect (imageURL, slug) from events and markets; only S3 URLs
	type item struct {
		url    string
		slug   string
		source string
	}
	var s3Items []item
	for _, e := range allEvents {
		slug := strings.TrimSpace(e.Slug)
		if u := strings.TrimSpace(e.Icon); u != "" && strings.HasPrefix(u, s3Base) {
			s3Items = append(s3Items, item{u, slug, "event.Icon"})
		}
		if u := strings.TrimSpace(e.Image); u != "" && strings.HasPrefix(u, s3Base) {
			s3Items = append(s3Items, item{u, slug, "event.Image"})
		}
		for _, m := range e.Markets {
			if u := strings.TrimSpace(m.Image); u != "" && strings.HasPrefix(u, s3Base) {
				s3Items = append(s3Items, item{u, slug, "market.Image"})
			}
		}
	}
	fmt.Printf("\nS3 image URLs from API: %d\n", len(s3Items))

	// HEAD each S3 URL (capped), record status and path pattern
	pathPatterns := make(map[string]int) // e.g. "slug.png" -> count
	pathStatus := make(map[string]int)   // path -> 200 or 404 count (we only store 200/404)
	var mu sync.Mutex
	headCount := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, headWorkers)
	client := &http.Client{Timeout: 8 * time.Second}

	for _, it := range s3Items {
		if headCount >= maxHEAD {
			break
		}
		headCount++
		u := it.url
		path := strings.TrimPrefix(u, s3Base)
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			req, _ := http.NewRequest(http.MethodHead, u, nil)
			resp, err := client.Do(req)
			status := 0
			if err == nil {
				status = resp.StatusCode
				resp.Body.Close()
			}
			mu.Lock()
			if status == 200 {
				pathStatus[path] = 200
				// Classify: exactly "slug.png", "slug-suffix.png", or other
				if strings.HasSuffix(path, ".png") {
					base := strings.TrimSuffix(path, ".png")
					if strings.Contains(base, "-") {
						// could be slug-suffix
						lastDash := strings.LastIndex(base, "-")
						suffix := base[lastDash+1:]
						if len(suffix) <= 20 && suffix != "" {
							pathPatterns["slug-suffix.png"]++
						} else {
							pathPatterns["other.png"]++
						}
					} else {
						pathPatterns["slug.png"]++
					}
				} else {
					pathPatterns["other"]++
				}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	fmt.Println("\n--- API S3 paths that returned 200 (pattern counts) ---")
	for p, c := range pathPatterns {
		fmt.Printf("  %s: %d\n", p, c)
	}

	// Build set of paths that returned 200 (for reference)
	paths200 := make(map[string]bool)
	for p, s := range pathStatus {
		if s == 200 {
			paths200[p] = true
		}
	}
	fmt.Printf("\nTotal distinct S3 paths that returned 200: %d\n", len(paths200))
	if len(paths200) > 0 {
		n := 0
		for p := range paths200 {
			if n >= 5 {
				fmt.Println("  ...")
				break
			}
			fmt.Printf("  %s\n", p)
			n++
		}
	}

	// Try derived URLs: base + slug + ".png" for events that have a slug
	slugTried := make(map[string]bool)
	var derived200, derived404 int
	derivedOther := make(map[int]int) // status code -> count
	var derivedErr int
	var derivedMu sync.Mutex
	clientNoRedirect := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, e := range allEvents {
		slug := strings.TrimSpace(e.Slug)
		if slug == "" || slugTried[slug] {
			continue
		}
		if derived200+derived404 >= maxHEAD/2 {
			break
		}
		slugTried[slug] = true
		derived := s3Base + slug + ".png"
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			req, _ := http.NewRequest(http.MethodHead, derived, nil)
			resp, err := clientNoRedirect.Do(req)
			derivedMu.Lock()
			defer derivedMu.Unlock()
			if err != nil {
				derivedErr++
				return
			}
			resp.Body.Close()
			if resp.StatusCode == 200 {
				derived200++
			} else if resp.StatusCode == 404 {
				derived404++
			} else {
				derivedOther[resp.StatusCode]++
			}
		}()
	}
	wg.Wait()

	fmt.Println("\n--- Derived URLs (base + slug + \".png\") ---")
	fmt.Printf("  Slug-only 200: %d\n", derived200)
	fmt.Printf("  Slug-only 404: %d\n", derived404)
	for code, c := range derivedOther {
		fmt.Printf("  Status %d: %d\n", code, c)
	}
	if derivedErr > 0 {
		fmt.Printf("  Errors (timeout/etc): %d\n", derivedErr)
	}
	fmt.Printf("  Slugs tried: %d\n", len(slugTried))
	if derivedOther[403] > 0 {
		fmt.Println("  (403 = S3 often uses this for missing key or no public read)")
	}
}

// gammaTeam is the team object returned by GET /teams (Gamma API).
type gammaTeam struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	League       string `json:"league"`
	Logo         string `json:"logo"`
	Abbreviation string `json:"abbreviation"`
	Alias        string `json:"alias"`
}

// runDiscoverTemplates fetches teams from Gamma /teams, collects S3 logo URLs per league,
// infers folder + {team} vs {abbrev}, HEAD-verifies, then prints SQL or updates DB.
func runDiscoverTemplates(updateDB bool) {
	// Fetch teams from Gamma /teams endpoint (source of truth for logos)
	teams := fetchGammaTeams(2000)
	if len(teams) == 0 {
		fmt.Println("no teams fetched from /teams")
		return
	}
	fmt.Printf("Fetched %d teams from Gamma /teams\n", len(teams))

	// Collect (league, folder, pathStem, teamName, teamAbbrev) from each team with S3 logo
	var samples []logoSample
	for _, t := range teams {
		logo := strings.TrimSpace(t.Logo)
		if logo == "" || !strings.HasPrefix(logo, s3Base) {
			continue
		}
		league := strings.TrimSpace(strings.ToLower(t.League))
		if league == "" {
			league = "sports"
		}
		path := strings.TrimPrefix(logo, s3Base)
		folder, stem := splitPathStem(path)
		if folder == "" || stem == "" {
			continue
		}
		samples = append(samples, logoSample{
			tagSlug: league,
			folder:  folder,
			stem:    stem,
			name:    strings.TrimSpace(t.Name),
			abbrev:  strings.TrimSpace(t.Abbreviation),
		})
	}

	// Group by (tagSlug, folder) -> list of samples
	type key struct{ tag, folder string }
	grouped := make(map[key][]logoSample)
	for _, s := range samples {
		k := key{s.tagSlug, s.folder}
		grouped[k] = append(grouped[k], s)
	}

	// Infer placeholder and build template per (tagSlug, folder); then HEAD-verify
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	verified := make(map[string]string) // tagSlug -> template
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, headWorkers)

	for k, list := range grouped {
		if len(list) == 0 {
			continue
		}
		// Prefer non-"sports" tag when we have a more specific one
		if k.tag == "sports" {
			hasOther := false
			for k2 := range grouped {
				if k2.folder == k.folder && k2.tag != "sports" {
					hasOther = true
					break
				}
			}
			if hasOther {
				continue
			}
		}

		useAbbrev := inferPlaceholder(k.folder, list)
		placeholder := "{team}"
		if useAbbrev {
			placeholder = "{abbrev}"
		}
		template := s3Base + k.folder + "/" + placeholder + ".png"

		// HEAD the actual sample URL (we know this path came from the API) to verify it returns 200
		testURL := s3Base + k.folder + "/" + list[0].stem + ".png"

		wg.Add(1)
		go func(tagSlug, tpl, checkURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			req, _ := http.NewRequest(http.MethodHead, checkURL, nil)
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode == 200 {
				mu.Lock()
				verified[tagSlug] = tpl
				mu.Unlock()
			}
		}(k.tag, template, testURL)
	}
	wg.Wait()

	// Prefer specific tag over "sports" when same template (e.g. keep "epl", drop "sports" for same folder)
	finalVerified := dedupeByPreferNonSports(verified)

	fmt.Println("\n--- Verified logo_url_template by league (tag slug) ---")
	if len(finalVerified) == 0 {
		fmt.Println("  (none verified with HEAD 200; check Gamma /teams returns teams with S3 logo URLs)")
	} else {
		for _, tag := range sortedKeys(finalVerified) {
			fmt.Printf("  %s -> %s\n", tag, finalVerified[tag])
		}
	}

	// Output SQL
	fmt.Println("\n--- SQL (run against polydata) ---")
	fmt.Println("-- UPDATE leagues SET logo_url_template = $template WHERE sport = $tag;")
	for _, tag := range sortedKeys(finalVerified) {
		tpl := finalVerified[tag]
		escaped := strings.ReplaceAll(tpl, "'", "''")
		fmt.Printf("UPDATE leagues SET logo_url_template = '%s' WHERE sport = '%s';\n", escaped, tag)
	}

	if updateDB {
		// Load .env so POSTGRES_* are set (same as main app). Try cwd, then parent dirs.
		if err := godotenv.Load(); err != nil {
			_ = godotenv.Load("../.env")
			_ = godotenv.Load("../../.env")
		}
		if err := applyTemplatesToDB(finalVerified); err != nil {
			fmt.Fprintf(os.Stderr, "update-db: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\nDB updated.")
	}
}

// fetchGammaTeams paginates GET /teams and returns all teams (up to max).
func fetchGammaTeams(max int) []gammaTeam {
	var out []gammaTeam
	offset := 0
	pageSize := 500
	for len(out) < max {
		path := fmt.Sprintf("/teams?limit=%d&offset=%d", pageSize, offset)
		resp, err := http.Get(gammaBase + path)
		if err != nil {
			break
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			break
		}
		var page []gammaTeam
		if err := json.Unmarshal(body, &page); err != nil {
			break
		}
		if len(page) == 0 {
			break
		}
		for _, t := range page {
			out = append(out, t)
			if len(out) >= max {
				return out
			}
		}
		offset += len(page)
		if len(page) < pageSize {
			break
		}
	}
	return out
}

func fetchSportsEvents(maxEvents int) []handlers.RawGammaEvent {
	var out []handlers.RawGammaEvent
	seen := make(map[string]bool)
	offset := 0
	for len(out) < maxEvents {
		path := fmt.Sprintf("/events?closed=false&active=true&archived=false&ascending=false&limit=%d&offset=%d&order=volume24hr&liquidity_min=200&volume_min=200&tag_id=100639&related_tags=true", limit, offset)
		resp, err := http.Get(gammaBase + path)
		if err != nil {
			break
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			break
		}
		var events []handlers.RawGammaEvent
		if err := json.Unmarshal(body, &events); err != nil {
			break
		}
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			if !seen[e.ID] {
				seen[e.ID] = true
				out = append(out, e)
				if len(out) >= maxEvents {
					return out
				}
			}
		}
		offset += len(events)
		if len(events) < limit {
			break
		}
	}
	return out
}

func splitPathStem(path string) (folder, stem string) {
	path = strings.Trim(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		return "", ""
	}
	folder = strings.Trim(parts[0], "/")
	rest := parts[1]
	if !strings.HasSuffix(rest, ".png") {
		return folder, ""
	}
	stem = strings.TrimSuffix(rest, ".png")
	return folder, stem
}

func normalizeToSlug(label string) string {
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

// inferPlaceholder returns true if path stems match abbreviation more often than normalized name.
func inferPlaceholder(folder string, list []logoSample) (useAbbrev bool) {
	var teamMatch, abbrevMatch int
	for _, s := range list {
		slug := normalizeToSlug(s.name)
		abbrevLower := strings.ToLower(strings.TrimSpace(s.abbrev))
		if slug != "" && s.stem == slug {
			teamMatch++
		}
		if abbrevLower != "" && s.stem == abbrevLower {
			abbrevMatch++
		}
	}
	return abbrevMatch > teamMatch
}

// dedupeByPreferNonSports returns one tag per template, preferring non-"sports" tag.
func dedupeByPreferNonSports(verified map[string]string) map[string]string {
	templateToTags := make(map[string][]string)
	for tag, tpl := range verified {
		templateToTags[tpl] = append(templateToTags[tpl], tag)
	}
	out := make(map[string]string)
	for tpl, tags := range templateToTags {
		var chosen string
		for _, t := range tags {
			if t != "sports" {
				chosen = t
				break
			}
			if chosen == "" {
				chosen = t
			}
		}
		if chosen != "" {
			out[chosen] = tpl
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func applyTemplatesToDB(verified map[string]string) error {
	if _, err := db.InitDB(); err != nil {
		return err
	}
	ctx := context.Background()
	for tag, tpl := range verified {
		_, err := db.Pool.Exec(ctx, `UPDATE leagues SET logo_url_template = $1 WHERE sport = $2`, tpl, tag)
		if err != nil {
			return fmt.Errorf("update league %q: %w", tag, err)
		}
	}
	return nil
}
