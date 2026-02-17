package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
)

func TestGetEvents(t *testing.T) {
	tests := []struct {
		name           string
		authEnabled    bool
		authHeader     string
		expectedStatus int
		expectBody     bool
	}{
		{
			name:           "success without auth",
			authEnabled:    false,
			expectedStatus: http.StatusOK,
			expectBody:     true,
		},
		{
			name:           "success with valid auth",
			authEnabled:    true,
			authHeader:     "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpYXQiOjE1MTYyMzkwMjIsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ.aBVuJ8rG3ZJV053YgdpP4K7wIcGfLJwaWNoEyt4Ps04",
			expectedStatus: http.StatusOK,
			expectBody:     true,
		},
		{
			name:           "unauthorized missing header",
			authEnabled:    true,
			expectedStatus: http.StatusUnauthorized,
			expectBody:     false,
		},
		{
			name:           "unauthorized invalid token",
			authEnabled:    true,
			authHeader:     "Bearer invalid.token.here",
			expectedStatus: http.StatusUnauthorized,
			expectBody:     false,
		},
		{
			name:           "unauthorized wrong format",
			authEnabled:    true,
			authHeader:     "Basic dXNlcjpwYXNz",
			expectedStatus: http.StatusUnauthorized,
			expectBody:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.authEnabled {
				os.Setenv("AUTH_ENABLED", "true")
				os.Setenv("JWT_SECRET", "test-secret-key")
			} else {
				os.Setenv("AUTH_ENABLED", "false")
			}
			defer func() {
				os.Unsetenv("AUTH_ENABLED")
				os.Unsetenv("JWT_SECRET")
			}()

			// Skip tests that require external API calls
			if tt.expectBody {
				t.Skip("Skipping test - requires external API connectivity")
			}

			// Create router
			r := chi.NewRouter()

			// Add auth middleware if enabled
			if tt.authEnabled {
				r.Use(middleware.Auth)
			}

			r.Get("/events", handlers.GetEvents)

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/events", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Serve request
			r.ServeHTTP(w, req)

			// Assert status
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectBody && w.Code == http.StatusOK {
				body := w.Body.Bytes()
				assert.NotEmpty(t, body)

				// Verify it's valid JSON array of CleanEvent objects
				var events []handlers.CleanEvent
				err := json.Unmarshal(body, &events)
				assert.NoError(t, err, "Response should be valid JSON array of CleanEvent")

				if err == nil && len(events) > 0 {
					// Verify structure of first event has expected CleanEvent fields
					event := events[0]
					assert.NotEmpty(t, event.ID, "Event should have id")
					assert.NotEmpty(t, event.Title, "Event should have title")
					assert.NotEmpty(t, event.Layout, "Event should have layout")
					assert.Contains(t, []string{"POLL", "SPORTS", "BINARY"}, event.Layout, "Layout should be valid")
					assert.NotNil(t, event.DisplayData, "Event should have displayData")
				}
			}
		})
	}
}

func TestGetTopNav(t *testing.T) {
	tests := []struct {
		name           string
		authEnabled    bool
		authHeader     string
		expectedStatus int
		expectBody     bool
	}{
		{
			name:           "success without auth",
			authEnabled:    false,
			expectedStatus: http.StatusOK,
			expectBody:     true,
		},
		{
			name:           "success with valid auth",
			authEnabled:    true,
			authHeader:     "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpYXQiOjE1MTYyMzkwMjIsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ.aBVuJ8rG3ZJV053YgdpP4K7wIcGfLJwaWNoEyt4Ps04",
			expectedStatus: http.StatusOK,
			expectBody:     true,
		},
		{
			name:           "unauthorized missing header",
			authEnabled:    true,
			expectedStatus: http.StatusUnauthorized,
			expectBody:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.authEnabled {
				os.Setenv("AUTH_ENABLED", "true")
				os.Setenv("JWT_SECRET", "test-secret-key")
			} else {
				os.Setenv("AUTH_ENABLED", "false")
			}
			defer func() {
				os.Unsetenv("AUTH_ENABLED")
				os.Unsetenv("JWT_SECRET")
			}()

			// Create router
			r := chi.NewRouter()

			// Add auth middleware if enabled
			if tt.authEnabled {
				r.Use(middleware.Auth)
			}

			r.Get("/top-nav", handlers.GetTopNav)

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/top-nav", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Serve request
			r.ServeHTTP(w, req)

			// Assert status
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectBody && w.Code == http.StatusOK {
				// Parse response body
				var navItems []handlers.NavItem
				err := json.Unmarshal(w.Body.Bytes(), &navItems)

				// Assert no error and correct structure
				assert.NoError(t, err)
				assert.Len(t, navItems, 3)
				assert.Equal(t, "Home", navItems[0].Label)
				assert.Equal(t, "home", navItems[0].Slug)
				expectedHomeRelated := []handlers.RelatedItem{
					{Label: "Welcome", Slug: "welcome"},
					{Label: "Dashboard", Slug: "dashboard"},
				}
				assert.Equal(t, expectedHomeRelated, navItems[0].Related)
				assert.Equal(t, "Events", navItems[1].Label)
				assert.Equal(t, "events", navItems[1].Slug)
				expectedEventsRelated := []handlers.RelatedItem{
					{Label: "Workshops", Slug: "workshops"},
					{Label: "Conferences", Slug: "conferences"},
				}
				assert.Equal(t, expectedEventsRelated, navItems[1].Related)
			}
		})
	}
}
