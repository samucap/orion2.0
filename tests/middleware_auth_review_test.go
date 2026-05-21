package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/require"
)

// TestAuthMiddleware_401ResponseBodiesDocumentBranches maps each 401 branch in
// middleware.Auth to the exact error payload clients should expect when debugging
// "login works but protected routes return 401".
func TestAuthMiddleware_401ResponseBodiesDocumentBranches(t *testing.T) {
	setAuthEnv(t)
	tb := db.TokenBlacklist.(*mockTokenBlacklist)

	t.Run("missing Authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"error":"Authorization header required"}`, strings.TrimSpace(w.Body.String()))
	})

	t.Run("not Bearer scheme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Basic x")
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"error":"Invalid authorization header format"}`, strings.TrimSpace(w.Body.String()))
	})

	t.Run("invalid token string", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer not-a-jwt")
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"error":"Invalid or expired token"}`, strings.TrimSpace(w.Body.String()))
	})

	t.Run("expired token", func(t *testing.T) {
		exp := time.Now().Add(-time.Hour)
		jti := "expired-jti"
		tokenStr := issueTestTokenCustom(t, 1, &jti, exp)
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"error":"Invalid or expired token"}`, strings.TrimSpace(w.Body.String()))
	})

	t.Run("missing jti claim", func(t *testing.T) {
		exp := time.Now().Add(15 * time.Minute)
		tokenStr := issueTestTokenCustom(t, 1, nil, exp)
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"error":"Invalid or expired token"}`, strings.TrimSpace(w.Body.String()))
	})

	t.Run("token revoked", func(t *testing.T) {
		jti := "revoked-for-review-test"
		exp := time.Now().Add(15 * time.Minute)
		tokenStr := issueTestTokenCustom(t, 42, &jti, exp)
		tb.mu.Lock()
		tb.blacklisted[jti] = true
		tb.mu.Unlock()
		t.Cleanup(func() {
			tb.mu.Lock()
			delete(tb.blacklisted, jti)
			tb.mu.Unlock()
		})

		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.JSONEq(t, `{"error":"Token revoked"}`, strings.TrimSpace(w.Body.String()))
	})

	t.Run("blacklist store error returns 500", func(t *testing.T) {
		tb.isBlacklistedErr = errBlacklistDown
		t.Cleanup(func() { tb.isBlacklistedErr = nil })

		tokenStr := issueTestToken(t, 99)
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		w := httptest.NewRecorder()
		middleware.Auth(okHandler()).ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.JSONEq(t, `{"error":"Internal server error"}`, strings.TrimSpace(w.Body.String()))
	})
}

// TestProtectedTopNav_WithBearerTokenSucceeds mirrors production: GET /api/top-nav
// behind middleware.Auth with Authorization: Bearer <token>.
func TestProtectedTopNav_WithBearerTokenSucceeds(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/api/top-nav", middleware.ValidateQuery(handlers.GetTopNav))

	tokenStr := issueTestToken(t, 7)
	req := httptest.NewRequest(http.MethodGet, "/api/top-nav", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), `"slug"`)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

var errBlacklistDown = errors.New("database unavailable")
