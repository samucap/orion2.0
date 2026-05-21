package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/db"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const testJWTSecret = "test-secret-key-minimum-length-for-security-1234"

type mockTokenBlacklist struct {
	mu          sync.Mutex
	blacklisted map[string]bool

	// If set, IsTokenBlacklisted returns an error to simulate DB failures.
	isBlacklistedErr error

	// Recorded args from the last BlacklistToken call (used for assertions).
	lastBlacklistJTI        string
	lastBlacklistUserID    int64
	lastBlacklistExpiresAt time.Time
}

func (m *mockTokenBlacklist) BlacklistToken(ctx context.Context, jti string, userID int64, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blacklisted == nil {
		m.blacklisted = make(map[string]bool)
	}
	m.blacklisted[jti] = true

	m.lastBlacklistJTI = jti
	m.lastBlacklistUserID = userID
	m.lastBlacklistExpiresAt = expiresAt

	return nil
}

func (m *mockTokenBlacklist) IsTokenBlacklisted(ctx context.Context, jti string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isBlacklistedErr != nil {
		return false, m.isBlacklistedErr
	}
	return m.blacklisted[jti], nil
}

func (m *mockTokenBlacklist) CleanupExpiredTokens(ctx context.Context) error {
	return nil
}

type mockUserStore struct {
	userByID map[int64]*db.User
}

func (m *mockUserStore) CreateUser(ctx context.Context, email, pw string) (*db.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mockUserStore) GetUserByEmail(ctx context.Context, email string) (*db.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mockUserStore) GetUserByID(ctx context.Context, userID int64) (*db.User, error) {
	if u, ok := m.userByID[userID]; ok {
		return u, nil
	}
	return nil, db.ErrUserNotFound
}
func (m *mockUserStore) UpdateLastLogin(ctx context.Context, userID int64) error {
	return nil
}
func (m *mockUserStore) UpdateUserEmail(ctx context.Context, userID int64, newEmail string) error {
	return nil
}
func (m *mockUserStore) UpdateUserPassword(ctx context.Context, userID int64, newHashedPW string) error {
	return nil
}
func (m *mockUserStore) DeleteUser(ctx context.Context, userID int64) error {
	return nil
}

// helper: issue a valid JWT for testing protected routes.
func issueTestToken(t *testing.T, userID int64) string {
	t.Helper()
	jti := fmt.Sprintf("jti-%d-%d", userID, time.Now().UnixNano())
	claims := jwt.MapClaims{
		"sub":  strconv.FormatInt(userID, 10),
		"jti":  jti,
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(15 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	return s
}

func issueTestTokenCustom(t *testing.T, userID int64, jti *string, exp time.Time) string {
	t.Helper()

	claims := jwt.MapClaims{
		"sub": strconv.FormatInt(userID, 10),
		"iat": time.Now().Unix(),
		"exp": exp.Unix(),
	}
	if jti != nil {
		claims["jti"] = *jti
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	return s
}

func setAuthEnv(t *testing.T) {
	t.Helper()
	origTokenBlacklist := db.TokenBlacklist
	tb := &mockTokenBlacklist{blacklisted: make(map[string]bool)}
	db.TokenBlacklist = tb
	t.Cleanup(func() {
		db.TokenBlacklist = origTokenBlacklist
	})

	os.Setenv("JWT_SECRET", testJWTSecret)
	os.Setenv("JWT_EXPIRY_MINUTES", "15")
	os.Setenv("BCRYPT_COST", "4") // low cost for fast tests
	t.Cleanup(func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("JWT_EXPIRY_MINUTES")
		os.Unsetenv("BCRYPT_COST")
	})
}

// ---------------------------------------------------------------------------
// Signup handler – validation tests (no database required)
// ---------------------------------------------------------------------------

func TestSignup_InvalidJSON(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/api/auth/signup", middleware.ValidateBody(handlers.Signup))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSignup_MissingFields(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/api/auth/signup", middleware.ValidateBody(handlers.Signup))

	body := `{"email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSignup_WeakPassword(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/api/auth/signup", middleware.ValidateBody(handlers.Signup))

	tests := []struct {
		name     string
		password string
	}{
		{"too short", "Abc!1234"},
		{"no uppercase", "abcdefghij1!"},
		{"no lowercase", "ABCDEFGHIJ1!"},
		{"no digit", "Abcdefghij!!"},
		{"no special char", "Abcdefghij12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{
				"email":    "test@example.com",
				"password": tt.password,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "password %q should be rejected", tt.password)
		})
	}
}

func TestSignup_InvalidEmail(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/api/auth/signup", middleware.ValidateBody(handlers.Signup))

	body, _ := json.Marshal(map[string]string{
		"email":    "not-an-email",
		"password": "Str0ng!Pass#2026",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSignup_Success_WithNullableAvatarSchema(t *testing.T) {
	setAuthEnv(t)

	// DB-dependent regression test for signup scan behavior when avatar can be NULL.
	candidates := []string{".env.test", "../.env.test"}
	loaded := ""
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if err := godotenv.Load(p); err != nil {
				t.Skipf("Skipping DB-dependent test: failed to load %s: %v", p, err)
			}
			loaded = p
			break
		}
	}
	if loaded == "" {
		t.Skip("Skipping DB-dependent test: .env.test not available in tests/ or project root")
	}

	if _, err := db.InitDB(); err != nil {
		t.Skipf("Database not available for testing: %v", err)
	}

	origUsers := db.Users
	db.Users = db.PgUserStore{}
	t.Cleanup(func() { db.Users = origUsers })

	email := fmt.Sprintf("signup-nullavatar-%d@example.com", time.Now().UnixNano())
	t.Cleanup(func() {
		if db.Pool != nil {
			_, _ = db.Pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
		}
	})

	r := chi.NewRouter()
	r.Post("/api/auth/signup", middleware.ValidateBody(handlers.Signup))

	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "Str0ng!Pass#2026",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	var resp handlers.AuthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)
	require.False(t, resp.ExpiresAt.IsZero())
}

// ---------------------------------------------------------------------------
// Login handler – validation tests (no database required)
// ---------------------------------------------------------------------------

func TestLogin_InvalidJSON(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/api/auth/login", middleware.ValidateBody(handlers.Login))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(`{bad`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLogin_MissingFields(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Post("/api/auth/login", middleware.ValidateBody(handlers.Login))

	body := `{"email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Auth middleware tests (no database required)
// ---------------------------------------------------------------------------

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer totally.invalid.token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	claims := jwt.MapClaims{
		"sub": "1",
		"iat": time.Now().Add(-1 * time.Hour).Unix(),
		"exp": time.Now().Add(-30 * time.Minute).Unix(), // expired
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.UserFromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"sub":   claims.Subject,
			"jti":   claims.ID,
		})
	})

	tokenStr := issueTestToken(t, 42)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "42", resp["sub"])
	assert.NotEmpty(t, resp["jti"])
}

func TestAuthMiddleware_WrongSigningKey(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Sign with a different secret
	claims := jwt.MapClaims{
		"sub": "1",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte("wrong-secret-key"))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_MissingJTI(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	exp := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	tokenStr := issueTestTokenCustom(t, 42, nil, exp)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_BlacklistStoreError(t *testing.T) {
	setAuthEnv(t)
	tb := db.TokenBlacklist.(*mockTokenBlacklist)
	tb.isBlacklistedErr = errors.New("db failure")

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tokenStr := issueTestToken(t, 42)

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ---------------------------------------------------------------------------
// Profile handler tests (no database required, just needs valid JWT context)
// ---------------------------------------------------------------------------

func TestProfile_ValidToken(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/api/protected/profile", handlers.Profile)

	origUsers := db.Users
	db.Users = &mockUserStore{
		userByID: map[int64]*db.User{
			7: {ID: 7, Email: "user@example.com"},
		},
	}
	t.Cleanup(func() {
		db.Users = origUsers
	})

	tokenStr := issueTestToken(t, 7)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/profile", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp handlers.ProfileResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "7", resp.UserID)
	assert.Equal(t, "user@example.com", resp.Email)
}

func TestProfile_NoToken(t *testing.T) {
	setAuthEnv(t)
	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Get("/api/protected/profile", handlers.Profile)

	req := httptest.NewRequest(http.MethodGet, "/api/protected/profile", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ---------------------------------------------------------------------------
// Token refresh/logout + revocation tests (no database required)
// ---------------------------------------------------------------------------

func TestRefreshToken_Valid(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/refresh", handlers.RefreshToken)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.UserFromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"sub": claims.Subject,
			"jti": claims.ID,
		})
	})

	origToken := issueTestToken(t, 42)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+origToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp handlers.AuthResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Token)

	// Verify refreshed token is accepted by middleware.
	req2 := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req2.Header.Set("Authorization", "Bearer "+resp.Token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	var secureResp map[string]string
	err = json.Unmarshal(w2.Body.Bytes(), &secureResp)
	require.NoError(t, err)
	assert.Equal(t, "42", secureResp["sub"])
	assert.NotEmpty(t, secureResp["jti"])
}

func TestRefreshToken_MissingHeader(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/refresh", handlers.RefreshToken)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRefreshToken_Expired(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/refresh", handlers.RefreshToken)

	claims := jwt.MapClaims{
		"sub":  "42",
		"jti":  "expired-jti",
		"iat":  time.Now().Add(-2 * time.Hour).Unix(),
		"exp":  time.Now().Add(-1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRefreshToken_BlacklistedRejected(t *testing.T) {
	setAuthEnv(t)
	tb := db.TokenBlacklist.(*mockTokenBlacklist)

	userID := int64(42)
	jti := fmt.Sprintf("revoked-jti-%d", time.Now().UnixNano())
	exp := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	tokenJTI := jti
	origToken := issueTestTokenCustom(t, userID, &tokenJTI, exp)

	tb.mu.Lock()
	tb.blacklisted[jti] = true
	tb.mu.Unlock()

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/refresh", handlers.RefreshToken)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+origToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogout_BlacklistsToken(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/logout", handlers.Logout)
	r.Get("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tokenStr := issueTestToken(t, 42)

	// Logout should succeed.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// The same token should be rejected immediately afterwards.
	req2 := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenStr)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestLogout_BlacklistTokenArgs(t *testing.T) {
	setAuthEnv(t)
	tb := db.TokenBlacklist.(*mockTokenBlacklist)

	userID := int64(42)
	jti := "logout-args-jti"
	exp := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	tokenJTI := jti
	tokenStr := issueTestTokenCustom(t, userID, &tokenJTI, exp)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/logout", handlers.Logout)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	assert.Equal(t, jti, tb.lastBlacklistJTI)
	assert.Equal(t, userID, tb.lastBlacklistUserID)
	assert.Equal(t, exp.Unix(), tb.lastBlacklistExpiresAt.Unix())
}

func TestLogout_SecondAttemptRejected(t *testing.T) {
	setAuthEnv(t)

	r := chi.NewRouter()
	r.Use(middleware.Auth)
	r.Post("/api/auth/logout", handlers.Logout)

	tokenStr := issueTestToken(t, 42)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Second logout with the already blacklisted token should be rejected.
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenStr)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestLogin_ValidCredentials_NullAvatarDoesNotBreakLogin(t *testing.T) {
	// This test reproduces the production bug where `users.avatar` is NULL,
	// which previously caused pgx scan into `string` to fail in `GetUserByEmail`.
	//
	// It is DB-dependent and will be skipped if the Postgres env files/tables
	// aren't available, or if the DB schema enforces `avatar` as NOT NULL.

	setAuthEnv(t)

	// Load DB credentials from a local env file when present.
	candidates := []string{".env.test", "../.env.test"}
	loaded := ""
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if err := godotenv.Load(p); err != nil {
				t.Skipf("Skipping DB-dependent test: failed to load %s: %v", p, err)
			}
			loaded = p
			break
		}
	}
	if loaded == "" {
		t.Skip("Skipping DB-dependent test: .env.test not available in tests/ or project root")
	}

	// Initialize database connection for testing.
	if _, err := db.InitDB(); err != nil {
		t.Skipf("Database not available for testing: %v", err)
	}

	origUsers := db.Users
	db.Users = db.PgUserStore{}
	t.Cleanup(func() { db.Users = origUsers })

	ctx := context.Background()

	const password = "Abcdef1!Ghij" // >=12 chars + upper/lower/digit/special
	hashedPW, err := bcrypt.GenerateFromPassword([]byte(password), 4) // matches test setup expectations
	require.NoError(t, err)

	email := fmt.Sprintf("nullavatar-%d@example.com", time.Now().UnixNano())

	// Insert a user with an explicit non-NULL avatar first, so the insert works
	// even if the column is NOT NULL. Then, try to force avatar to NULL.
	var userID int64
	insertQ := `INSERT INTO users (email, pw, avatar) VALUES ($1, $2, $3) RETURNING id`
	require.NoError(t, db.Pool.QueryRow(ctx, insertQ, email, string(hashedPW), "").Scan(&userID))

	inserted := true
	t.Cleanup(func() {
		if !inserted || db.Pool == nil {
			return
		}
		_, _ = db.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	_, err = db.Pool.Exec(ctx, `UPDATE users SET avatar = NULL WHERE id = $1`, userID)
	if err != nil {
		t.Skipf("Skipping NULL-avatar regression test: could not set avatar to NULL (schema likely NOT NULL): %v", err)
	}

	u, err := db.Users.GetUserByEmail(ctx, email)
	require.NoError(t, err)
	require.Equal(t, userID, u.ID)
	require.Equal(t, "", u.Avatar, "avatar should normalize NULL to empty string")

	r := chi.NewRouter()
	r.Post("/api/auth/login", middleware.ValidateBody(handlers.Login))

	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp handlers.AuthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)
}
