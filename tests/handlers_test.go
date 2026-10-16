package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
)

func TestGetTopNav(t *testing.T) {
	validToken := issueTestToken(t, 1234567890)
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
			authHeader:     "Bearer " + validToken,
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
				os.Setenv("JWT_SECRET", testJWTSecret)
				origBlacklist := db.TokenBlacklist
				db.TokenBlacklist = &mockTokenBlacklist{}
				t.Cleanup(func() { db.TokenBlacklist = origBlacklist })
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
				r.Use(middleware.Auth(testAuthConfig(t)))
			}

			r.Get("/top-nav", middleware.ValidateQuery(handlers.GetTopNav))

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
