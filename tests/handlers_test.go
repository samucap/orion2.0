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
			authHeader:     "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
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
				// Parse response body
				var events []handlers.Event
				err := json.Unmarshal(w.Body.Bytes(), &events)

				// Assert no error and correct structure
				assert.NoError(t, err)
				assert.Len(t, events, 2)
				assert.Equal(t, "Tech Summit 2026", events[0].Title)
				assert.Equal(t, "AI Workshop", events[1].Title)
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
			authHeader:     "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
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
				assert.Equal(t, "Events", navItems[1].Label)
				assert.Equal(t, "About", navItems[2].Label)
			}
		})
	}
}
