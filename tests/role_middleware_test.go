package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_MissingRole(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth(testAuthConfig(t)))
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	claims := jwt.MapClaims{
		"sub": "1",
		"jti": "jti-no-role",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireRole_DemoForbiddenUserAllowed(t *testing.T) {
	setAuthEnv(t)
	cfg := testAuthConfig(t)
	reached := false
	r := chi.NewRouter()
	r.Use(middleware.Auth(cfg))
	r.With(middleware.RequireRole(auth.RoleUser)).Put("/profile", func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	demo, _, err := auth.IssueAccessToken(cfg, 7, auth.RoleDemo)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/profile", nil)
	req.Header.Set("Authorization", "Bearer "+demo)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, reached)

	user, _, err := auth.IssueAccessToken(cfg, 7, auth.RoleUser)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPut, "/profile", nil)
	req.Header.Set("Authorization", "Bearer "+user)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, reached)
}
