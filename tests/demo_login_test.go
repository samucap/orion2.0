package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const demoEmail = "demo_plz@demo.com"

type demoLookupStore struct {
	user *db.User
}

func (s demoLookupStore) CreateUser(context.Context, string, string) (*db.User, error) {
	return nil, db.ErrUserNotFound
}
func (s demoLookupStore) GetUserByEmail(_ context.Context, email string) (*db.User, error) {
	if s.user == nil || s.user.Email != email {
		return nil, db.ErrUserNotFound
	}
	return s.user, nil
}
func (s demoLookupStore) GetUserByID(context.Context, int64) (*db.User, error) {
	return nil, db.ErrUserNotFound
}
func (s demoLookupStore) UpdateLastLogin(context.Context, int64) error { return nil }
func (s demoLookupStore) UpdateUserEmail(context.Context, int64, string) error {
	return nil
}
func (s demoLookupStore) UpdateUserPassword(context.Context, int64, string) error {
	return nil
}
func (s demoLookupStore) DeleteUser(context.Context, int64) error { return nil }

func setupDemoLogin(t *testing.T, user *db.User) (*chi.Mux, *mockRefreshStore) {
	t.Helper()
	setAuthEnv(t)
	t.Setenv("DEMO_USER_EMAIL", demoEmail)
	t.Setenv("DEMO_USER_PW", "server-only-secret")

	orig := db.Users
	db.Users = demoLookupStore{user: user}
	t.Cleanup(func() { db.Users = orig })

	cfg := testAuthConfig(t)
	store := newMockRefreshStore()
	svc := auth.NewRefreshService(store, cfg)
	handlers.SetRefreshService(svc)
	t.Cleanup(func() { handlers.SetRefreshService(nil) })

	r := chi.NewRouter()
	r.Post("/api/auth/demo", middleware.ValidateBody(handlers.DemoLogin))
	return r, store
}

func postDemo(t *testing.T, r http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/demo", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TestAgent")
	req.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestDemoLogin_EmptyBodyIssuesDemoSession(t *testing.T) {
	user := &db.User{ID: 9, Email: demoEmail, Role: auth.RoleDemo}
	router, store := setupDemoLogin(t, user)

	w := postDemo(t, router, `{}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp handlers.AuthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	parsed, err := jwt.Parse(resp.Token, func(*jwt.Token) (interface{}, error) {
		return []byte(testJWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err)
	claims := parsed.Claims.(jwt.MapClaims)
	assert.Equal(t, "9", claims["sub"])
	assert.Equal(t, auth.RoleDemo, claims["role"])

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "refreshToken" {
			cookie = c
		}
	}
	require.NotNil(t, cookie)
	assert.True(t, cookie.HttpOnly)
	assert.GreaterOrEqual(t, cookie.MaxAge, 3500)
	assert.LessOrEqual(t, cookie.MaxAge, 3600)

	row, err := store.GetByTokenHash(context.Background(), hashToken(cookie.Value))
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, user.ID, row.UserID)
	assert.WithinDuration(t, time.Now().Add(time.Hour), row.ExpiresAt, 5*time.Second)
}

func TestDemoLogin_UnknownFieldsRejected(t *testing.T) {
	user := &db.User{ID: 9, Email: demoEmail, Role: auth.RoleDemo}
	router, store := setupDemoLogin(t, user)

	bodies := []string{
		`{"email":"other@example.com"}`,
		`{"password":"a valid password string"}`,
		`{"role":"user"}`,
		`{"email":"other@example.com","password":"a valid password string","role":"user"}`,
	}
	for _, body := range bodies {
		w := postDemo(t, router, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, body)
	}
	assert.Empty(t, store.tokens)
}

func TestDemoLogin_NotConfigured(t *testing.T) {
	setAuthEnv(t)
	t.Setenv("DEMO_USER_EMAIL", "")
	t.Setenv("DEMO_USER_PW", "")

	r := chi.NewRouter()
	r.Post("/api/auth/demo", middleware.ValidateBody(handlers.DemoLogin))
	w := postDemo(t, r, `{}`)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	user := &db.User{ID: 9, Email: demoEmail, Role: auth.RoleUser}
	router, _ := setupDemoLogin(t, user)
	w = postDemo(t, router, `{}`)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "non-demo role must not sign in")
}
