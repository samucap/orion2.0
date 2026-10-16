package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// RequireOrigin blocks cross-site requests to cookie-authenticated endpoints (CSRF).
//
// Allowed entries are exact origins ("https://app.example.com") or a
// "scheme://host:*" pattern that matches any port.
//
// Browsers always send Origin on POST, and pages cannot forge it, so a present
// Origin must match. "null" (sandboxed or no-referrer contexts) never matches.
// Without Origin, a browser-supplied Sec-Fetch-Site other than same-origin or
// none is rejected. Requests with neither header come from non-browser clients,
// which cannot carry a victim's cookies, so they pass.
func RequireOrigin(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" {
				if !originAllowed(origin, allowed) {
					slog.Warn("Rejected request from disallowed origin", "origin", origin, "path", r.URL.Path)
					http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
					return
				}
			} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				slog.Warn("Rejected cross-site request without Origin", "sec_fetch_site", site, "path", r.URL.Path)
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func originAllowed(origin string, allowed []string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	for _, a := range allowed {
		if base, ok := strings.CutSuffix(a, ":*"); ok {
			p, err := url.Parse(base)
			if err == nil && strings.EqualFold(u.Scheme, p.Scheme) && strings.EqualFold(u.Hostname(), p.Hostname()) {
				return true
			}
			continue
		}
		if strings.EqualFold(origin, strings.TrimRight(a, "/")) {
			return true
		}
	}
	return false
}
