// verify_country_flags tries derived country-flag URLs (country-flags/{abbrev}.png)
// with multiple abbreviation variants per country, and reports which return HTTP 200.
// Use the output to fix handlers/enrich.go countryNameToCode or DB team logos.
//
// Run from repo root: go run ./scripts/verify_country_flags/
package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	s3Base     = "https://polymarket-upload.s3.us-east-2.amazonaws.com/country-flags/"
	concurrent = 10
)

// countryVariants: country name (for display) -> abbreviation variants to try (lowercase).
// Order: prefer FIFA 3-letter first, then ISO 2-letter if different.
var countryVariants = map[string][]string{
	"Afghanistan":   {"afg", "af"},
	"Albania":       {"alb", "al"},
	"Algeria":       {"alg", "dz"},
	"Argentina":     {"arg", "ar"},
	"Australia":     {"aus", "au"},
	"Austria":       {"aut", "at"},
	"Belgium":       {"bel", "be"},
	"Bolivia":       {"bol", "bo"},
	"Bosnia and Herzegovina": {"bih", "ba"},
	"Brazil":        {"bra", "br"},
	"Bulgaria":      {"bul", "bg"},
	"Cameroon":      {"cmr", "cm"},
	"Canada":        {"can", "ca"},
	"Chile":         {"chi", "cl"},
	"China":         {"chn", "cn"},
	"Colombia":      {"col", "co"},
	"Costa Rica":    {"crc", "cr"},
	"Croatia":       {"cro", "hr"},
	"Czech Republic": {"cze", "cz"},
	"Denmark":       {"den", "dk"},
	"Ecuador":       {"ecu", "ec"},
	"Egypt":         {"egy", "eg"},
	"England":       {"eng", "gb-eng"},
	"Finland":       {"fin", "fi"},
	"France":        {"fra", "fr"},
	"Germany":       {"ger", "deu", "de"},
	"Ghana":         {"gha", "gh"},
	"Greece":        {"gre", "gr"},
	"Hungary":       {"hun", "hu"},
	"Iceland":       {"isl", "is"},
	"Iran":          {"irn", "ir"},
	"Italy":         {"ita", "it"},
	"Ivory Coast":   {"civ", "ci"},
	"Jamaica":       {"jam", "jm"},
	"Japan":         {"jpn", "jp"},
	"South Korea":   {"kor", "kr"},
	"North Korea":   {"prk", "kp"},
	"Mexico":        {"mex", "mx"},
	"Morocco":       {"mar", "ma"},
	"Netherlands":   {"ned", "nld", "nl"},
	"New Zealand":   {"nzl", "nz"},
	"Nigeria":       {"nga", "ng"},
	"Norway":        {"nor", "no"},
	"Paraguay":      {"par", "py"},
	"Peru":          {"per", "pe"},
	"Poland":        {"pol", "pl"},
	"Portugal":      {"por", "pt"},
	"Republic of Ireland": {"irl", "ie"},
	"Ireland":       {"irl", "ie"},
	"Romania":       {"rou", "ro"},
	"Russia":        {"rus", "ru"},
	"Saudi Arabia":  {"sau", "sa"},
	"Scotland":      {"sco", "gb-sct"},
	"Senegal":       {"sen", "sn"},
	"Serbia":        {"srb", "rs"},
	"Slovakia":      {"svk", "sk"},
	"Slovenia":      {"svn", "si"},
	"South Africa":  {"rsa", "za"},
	"Spain":         {"esp", "es"},
	"Sweden":        {"swe", "se"},
	"Switzerland":  {"sui", "che", "ch"},
	"Tunisia":       {"tun", "tn"},
	"Turkey":        {"tur", "tr"},
	"Ukraine":       {"ukr", "ua"},
	"United Arab Emirates": {"uae", "ae"},
	"United States": {"usa", "us"},
	"USA":           {"usa", "us"},
	"Uruguay":       {"uru", "uy"},
	"Venezuela":     {"ven", "ve"},
	"Wales":         {"wal", "gb-wls"},
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var mu sync.Mutex
	ok := make(map[string]string)   // country name -> abbrev that returned 200
	fail := make(map[string][]string) // country name -> abbrevs tried, all failed
	sem := make(chan struct{}, concurrent)
	var wg sync.WaitGroup

	for country, variants := range countryVariants {
		for _, ab := range variants {
			ab := ab
			country := country
			url := s3Base + ab + ".png"
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				req, _ := http.NewRequest(http.MethodHead, url, nil)
				resp, err := client.Do(req)
				if err != nil {
					mu.Lock()
					fail[country] = append(fail[country], ab+" (err)")
					mu.Unlock()
					return
				}
				resp.Body.Close()
				mu.Lock()
				if resp.StatusCode == 200 {
					if _, exists := ok[country]; !exists {
						ok[country] = ab
					}
				} else {
					fail[country] = append(fail[country], ab)
				}
				mu.Unlock()
			}()
		}
	}
	wg.Wait()

	// Report 200s
	var names []string
	for n := range ok {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println("--- country-flags URLs that returned 200 ---")
	fmt.Printf("%d countries with at least one working abbrev:\n\n", len(names))
	for _, n := range names {
		fmt.Printf("  %q -> %q   (%s%s.png)\n", n, ok[n], s3Base, ok[n])
	}

	// Suggested Go map for handlers/enrich.go
	fmt.Println("\n--- Suggested countryNameToCode map (lowercase keys) ---")
	fmt.Println("var countryNameToCode = map[string]string{")
	for _, n := range names {
		key := strings.ToLower(n)
		fmt.Printf("\t%q: %q,\n", key, ok[n])
	}
	fmt.Println("}")

	// Countries with no 200 (optional: show what we tried)
	var noHit []string
	for country := range countryVariants {
		if _, has := ok[country]; !has {
			noHit = append(noHit, country)
		}
	}
	sort.Strings(noHit)
	if len(noHit) > 0 {
		fmt.Printf("\n--- %d countries with no 200 (tried: %v etc.) ---\n", len(noHit), fail[noHit[0]])
		for _, n := range noHit {
			fmt.Printf("  %s tried: %v\n", n, fail[n])
		}
	}
}
