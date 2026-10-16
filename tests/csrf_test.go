package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
)

func TestRequireOrigin(t *testing.T) {
	prod := []string{"https://app.example.com"}
	dev := []string{"http://localhost:*"}

	tests := []struct {
		name    string
		allowed []string
		origin  string
		site    string
		want    int
	}{
		{"matching origin", prod, "https://app.example.com", "same-origin", http.StatusOK},
		{"origin case-insensitive", prod, "https://APP.example.com", "", http.StatusOK},
		{"foreign origin", prod, "https://evil.com", "cross-site", http.StatusForbidden},
		{"sibling subdomain", prod, "https://evil.example.com", "same-site", http.StatusForbidden},
		{"scheme mismatch", prod, "http://app.example.com", "", http.StatusForbidden},
		{"port mismatch", prod, "https://app.example.com:8443", "", http.StatusForbidden},
		{"suffix trick", prod, "https://app.example.com.evil.com", "", http.StatusForbidden},
		{"null origin", prod, "null", "cross-site", http.StatusForbidden},
		{"no origin, cross-site fetch metadata", prod, "", "cross-site", http.StatusForbidden},
		{"no origin, same-site fetch metadata", prod, "", "same-site", http.StatusForbidden},
		{"no origin, same-origin fetch metadata", prod, "", "same-origin", http.StatusOK},
		{"non-browser client", prod, "", "", http.StatusOK},
		{"dev localhost any port", dev, "http://localhost:3000", "", http.StatusOK},
		{"dev rejects other host", dev, "http://evil.com:3000", "", http.StatusForbidden},
		{"dev rejects https localhost", dev, "https://localhost:3000", "", http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := middleware.RequireOrigin(tc.allowed)(okHandler())
			req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh-token", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.site != "" {
				req.Header.Set("Sec-Fetch-Site", tc.site)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestAllowContentType_JSONOnly(t *testing.T) {
	h := chiMiddleware.AllowContentType("application/json")(okHandler())
	body := `{"email":"a@example.com","password":"x"}`

	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"json", "application/json", body, http.StatusOK},
		{"json with charset", "application/json; charset=utf-8", body, http.StatusOK},
		{"text/plain form", "text/plain", body, http.StatusUnsupportedMediaType},
		{"urlencoded form", "application/x-www-form-urlencoded", "email=a", http.StatusUnsupportedMediaType},
		{"multipart form", "multipart/form-data; boundary=x", body, http.StatusUnsupportedMediaType},
		{"missing content type", "", body, http.StatusUnsupportedMediaType},
		{"no body", "", "", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
}
