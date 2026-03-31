package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/samucap/orion2.0/handlers"
	"github.com/samucap/orion2.0/internal/auth"
	"github.com/samucap/orion2.0/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const refreshTestSecret = "test-secret-key-minimum-length-for-security-1234"

// ---------------------------------------------------------------------------
// Mock store (implements auth.RefreshTokenStore)
// ---------------------------------------------------------------------------

type mockRefreshStore struct {
	mu     sync.Mutex
	tokens map[string]*auth.RefreshToken
	nextID int64
}

func newMockRefreshStore() *mockRefreshStore {
	return &mockRefreshStore{tokens: make(map[string]*auth.RefreshToken)}
}

func (m *mockRefreshStore) InsertRefreshToken(_ context.Context, tokenHash string, userID int64, fingerprint string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.tokens[tokenHash] = &auth.RefreshToken{
		ID:                m.nextID,
		TokenHash:         tokenHash,
		UserID:            userID,
		ExpiresAt:         expiresAt,
		Revoked:           false,
		DeviceFingerprint: fingerprint,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	return nil
}

func (m *mockRefreshStore) GetByTokenHash(_ context.Context, tokenHash string) (*auth.RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[tokenHash]
	if !ok {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (m *mockRefreshStore) RevokeByID(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.ID == id {
			t.Revoked = true
			return nil
		}
	}
	return nil
}

func (m *mockRefreshStore) RevokeAllForUser(_ context.Context, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.UserID == userID {
			t.Revoked = true
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func seedRefreshToken(store *mockRefreshStore, raw string, userID int64, fingerprint string, expiresAt time.Time) {
	store.InsertRefreshToken(context.Background(), hashToken(raw), userID, fingerprint, expiresAt)
}

func setupRefreshTest(t *testing.T) (*chi.Mux, *mockRefreshStore) {
	t.Helper()
	t.Setenv("JWT_SECRET", refreshTestSecret)
	t.Setenv("JWT_EXPIRY_MINUTES", "15")

	store := newMockRefreshStore()
	svc := auth.NewRefreshService(store)
	handlers.SetRefreshService(svc)
	t.Cleanup(func() { handlers.SetRefreshService(nil) })

	r := chi.NewRouter()
	r.Route("/auth", func(r chi.Router) {
		r.With(middleware.RefreshToken).Post("/refresh-token", handlers.OpaqueRefresh)
		r.With(middleware.RefreshToken).Post("/logout-token", handlers.OpaqueLogout)
	})
	return r, store
}

func refreshReq(method, path, cookieVal string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("User-Agent", "TestAgent")
	req.Header.Set("Accept-Language", "en")
	if cookieVal != "" {
		req.AddCookie(&http.Cookie{Name: "refreshToken", Value: cookieVal})
	}
	return req
}

func computeFingerprint(r *http.Request) string {
	data := r.Header.Get("User-Agent") +
		"|" + r.Header.Get("Accept-Language") +
		"|" + r.Header.Get("X-Forwarded-For")
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRefresh_SuccessfulRotation verifies the happy path: a valid refresh
// token yields a new access JWT, a new refresh cookie, and the old token
// is revoked in the store.
func TestRefresh_SuccessfulRotation(t *testing.T) {
	router, store := setupRefreshTest(t)

	rawOld := "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344"
	req := refreshReq("POST", "/auth/refresh-token", rawOld)
	fp := computeFingerprint(req)
	seedRefreshToken(store, rawOld, 42, fp, time.Now().Add(24*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "expected 200 on valid refresh")

	body := w.Body.String()
	assert.Contains(t, body, `"token"`)
	assert.Contains(t, body, `"expires_at"`)
	assert.NotContains(t, body, rawOld, "raw refresh token must not leak into JSON")

	var refreshCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "refreshToken" {
			refreshCookie = c
			break
		}
	}
	require.NotNil(t, refreshCookie, "response must set a new refreshToken cookie")
	assert.NotEqual(t, rawOld, refreshCookie.Value, "new cookie must differ from old token")
	assert.True(t, refreshCookie.HttpOnly)
	assert.True(t, refreshCookie.Secure)
	assert.Equal(t, http.SameSiteStrictMode, refreshCookie.SameSite)

	oldRow, _ := store.GetByTokenHash(context.Background(), hashToken(rawOld))
	require.NotNil(t, oldRow)
	assert.True(t, oldRow.Revoked, "old token should be revoked after rotation")

	token := extractJWT(t, body)
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(refreshTestSecret), nil
	})
	require.NoError(t, err)
	claims := parsed.Claims.(*jwt.RegisteredClaims)
	assert.Equal(t, "42", claims.Subject)
}

// TestRefresh_OldTokenRejected verifies that presenting an already-rotated
// (revoked) token returns 401 and causes all of the user's tokens to be
// revoked (token family kill).
func TestRefresh_OldTokenRejected(t *testing.T) {
	router, store := setupRefreshTest(t)

	rawOld := "deadbeef00000000deadbeef00000000deadbeef00000000deadbeef00000000"
	req := refreshReq("POST", "/auth/refresh-token", rawOld)
	fp := computeFingerprint(req)

	seedRefreshToken(store, rawOld, 99, fp, time.Now().Add(24*time.Hour))
	store.mu.Lock()
	store.tokens[hashToken(rawOld)].Revoked = true
	store.mu.Unlock()

	rawOther := "11111111222222223333333344444444aaaabbbbccccddddeeee000011112222"
	seedRefreshToken(store, rawOther, 99, fp, time.Now().Add(24*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	sibling, _ := store.GetByTokenHash(context.Background(), hashToken(rawOther))
	require.NotNil(t, sibling)
	assert.True(t, sibling.Revoked, "sibling token should be revoked after replay detection")
}

// TestRefresh_MissingCookie verifies that a request without the
// refreshToken cookie is rejected with 401 by the middleware.
func TestRefresh_MissingCookie(t *testing.T) {
	router, _ := setupRefreshTest(t)

	req := refreshReq("POST", "/auth/refresh-token", "")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRefresh_ExpiredToken verifies that an expired (but not revoked)
// token is rejected with 401.
func TestRefresh_ExpiredToken(t *testing.T) {
	router, store := setupRefreshTest(t)

	rawExpired := "eeeeeeee00000000eeeeeeee00000000eeeeeeee00000000eeeeeeee00000000"
	req := refreshReq("POST", "/auth/refresh-token", rawExpired)
	fp := computeFingerprint(req)
	seedRefreshToken(store, rawExpired, 7, fp, time.Now().Add(-1*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRefresh_DeviceFingerprintChange verifies that presenting a valid
// token from a different device (changed fingerprint) returns 401 and
// revokes all tokens for the user.
func TestRefresh_DeviceFingerprintChange(t *testing.T) {
	router, store := setupRefreshTest(t)

	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	seedRefreshToken(store, rawToken, 55, "original-fingerprint-hash", time.Now().Add(24*time.Hour))

	req := refreshReq("POST", "/auth/refresh-token", rawToken)
	req.Header.Set("User-Agent", "DifferentBrowser/1.0")
	req.Header.Set("Accept-Language", "fr")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	row, _ := store.GetByTokenHash(context.Background(), hashToken(rawToken))
	require.NotNil(t, row)
	assert.True(t, row.Revoked, "token should be revoked on fingerprint mismatch")
}

// TestLogout_RevokesAndClearsCookie verifies that the logout endpoint
// revokes the token and clears the cookie.
func TestLogout_RevokesAndClearsCookie(t *testing.T) {
	router, store := setupRefreshTest(t)

	rawToken := "bbbbbbbb00000000bbbbbbbb00000000bbbbbbbb00000000bbbbbbbb00000000"
	req := refreshReq("POST", "/auth/logout-token", rawToken)
	fp := computeFingerprint(req)
	seedRefreshToken(store, rawToken, 10, fp, time.Now().Add(24*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	row, _ := store.GetByTokenHash(context.Background(), hashToken(rawToken))
	require.NotNil(t, row)
	assert.True(t, row.Revoked, "token should be revoked after logout")

	var cleared *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "refreshToken" {
			cleared = c
			break
		}
	}
	require.NotNil(t, cleared, "logout should clear the refreshToken cookie")
	assert.Equal(t, "", cleared.Value)
	assert.True(t, cleared.MaxAge < 0, "MaxAge should be negative to expire the cookie")
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func extractJWT(t *testing.T, body string) string {
	t.Helper()
	const prefix = `"token":"`
	start := 0
	for i := 0; i+len(prefix) <= len(body); i++ {
		if body[i:i+len(prefix)] == prefix {
			start = i + len(prefix)
			break
		}
	}
	require.NotZero(t, start, "could not find token in response body")
	end := start
	for end < len(body) && body[end] != '"' {
		end++
	}
	return body[start:end]
}
