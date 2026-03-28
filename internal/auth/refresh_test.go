package auth

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-minimum-length-for-security-1234"

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockRefreshTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*RefreshToken // keyed by token_hash
	nextID int64
}

func newMockStore() *mockRefreshTokenStore {
	return &mockRefreshTokenStore{tokens: make(map[string]*RefreshToken)}
}

func (m *mockRefreshTokenStore) InsertRefreshToken(_ context.Context, tokenHash string, userID int64, fingerprint string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.tokens[tokenHash] = &RefreshToken{
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

func (m *mockRefreshTokenStore) GetByTokenHash(_ context.Context, tokenHash string) (*RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[tokenHash]
	if !ok {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (m *mockRefreshTokenStore) RevokeByID(_ context.Context, id int64) error {
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

func (m *mockRefreshTokenStore) RevokeAllForUser(_ context.Context, userID int64) error {
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

func setupTestService(t *testing.T) (*RefreshService, *mockRefreshTokenStore) {
	t.Helper()
	t.Setenv("JWT_SECRET", testSecret)
	t.Setenv("JWT_EXPIRY_MINUTES", "15")

	store := newMockStore()
	svc := newRefreshServiceWithSecret(store, testSecret)
	return svc, store
}

func buildRouter(svc *RefreshService) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/auth", func(r chi.Router) {
		r.With(RefreshTokenMiddleware()).Post("/refresh-token", RefreshHandler(svc))
		r.With(RefreshTokenMiddleware()).Post("/logout-token", LogoutHandler(svc))
	})
	return r
}

func hashRaw(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func seedToken(store *mockRefreshTokenStore, raw string, userID int64, fingerprint string, expiresAt time.Time) {
	store.InsertRefreshToken(context.Background(), hashRaw(raw), userID, fingerprint, expiresAt)
}

const testFingerprint = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func reqWithCookie(method, path, cookieVal string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("User-Agent", "TestAgent")
	req.Header.Set("Accept-Language", "en")
	if cookieVal != "" {
		req.AddCookie(&http.Cookie{Name: "refreshToken", Value: cookieVal})
	}
	return req
}

func computeTestFingerprint(r *http.Request) string {
	svc := &RefreshService{}
	return svc.ComputeDeviceFingerprint(r)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRefresh_SuccessfulRotation verifies the happy path: a valid refresh
// token yields a new access JWT, a new refresh cookie, and the old token
// is revoked in the store.
func TestRefresh_SuccessfulRotation(t *testing.T) {
	svc, store := setupTestService(t)
	router := buildRouter(svc)

	rawOld := "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344"
	req := reqWithCookie("POST", "/auth/refresh-token", rawOld)
	fp := computeTestFingerprint(req)
	seedToken(store, rawOld, 42, fp, time.Now().Add(24*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "expected 200 on valid refresh")

	// Body must contain an access JWT.
	body := w.Body.String()
	assert.Contains(t, body, `"token"`)
	assert.Contains(t, body, `"expires_at"`)
	assert.NotContains(t, body, rawOld, "raw refresh token must not leak into JSON")

	// New refresh cookie must be set.
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

	// Old token must be revoked in the store.
	oldRow, _ := store.GetByTokenHash(context.Background(), hashRaw(rawOld))
	require.NotNil(t, oldRow)
	assert.True(t, oldRow.Revoked, "old token should be revoked after rotation")

	// Verify the access JWT is parseable and has the right subject.
	parsed, err := jwt.ParseWithClaims(body[:0]+extractToken(t, body), &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	})
	require.NoError(t, err)
	claims := parsed.Claims.(*jwt.RegisteredClaims)
	assert.Equal(t, "42", claims.Subject)
}

// TestRefresh_OldTokenRejected verifies that presenting an already-rotated
// (revoked) token returns 401 and causes all of the user's tokens to be
// revoked (token family kill).
func TestRefresh_OldTokenRejected(t *testing.T) {
	svc, store := setupTestService(t)
	router := buildRouter(svc)

	rawOld := "deadbeef00000000deadbeef00000000deadbeef00000000deadbeef00000000"
	req := reqWithCookie("POST", "/auth/refresh-token", rawOld)
	fp := computeTestFingerprint(req)

	// Seed two tokens for the same user; mark the first as revoked.
	seedToken(store, rawOld, 99, fp, time.Now().Add(24*time.Hour))
	store.mu.Lock()
	store.tokens[hashRaw(rawOld)].Revoked = true
	store.mu.Unlock()

	rawOther := "11111111222222223333333344444444aaaabbbbccccddddeeee000011112222"
	seedToken(store, rawOther, 99, fp, time.Now().Add(24*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// The sibling token must also be revoked (family kill).
	sibling, _ := store.GetByTokenHash(context.Background(), hashRaw(rawOther))
	require.NotNil(t, sibling)
	assert.True(t, sibling.Revoked, "sibling token should be revoked after replay detection")
}

// TestRefresh_MissingCookie verifies that a request without the
// refreshToken cookie is rejected with 401 by the middleware.
func TestRefresh_MissingCookie(t *testing.T) {
	svc, _ := setupTestService(t)
	router := buildRouter(svc)

	req := reqWithCookie("POST", "/auth/refresh-token", "")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRefresh_ExpiredToken verifies that an expired (but not revoked)
// token is rejected with 401.
func TestRefresh_ExpiredToken(t *testing.T) {
	svc, store := setupTestService(t)
	router := buildRouter(svc)

	rawExpired := "eeeeeeee00000000eeeeeeee00000000eeeeeeee00000000eeeeeeee00000000"
	req := reqWithCookie("POST", "/auth/refresh-token", rawExpired)
	fp := computeTestFingerprint(req)
	seedToken(store, rawExpired, 7, fp, time.Now().Add(-1*time.Hour))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRefresh_DeviceFingerprintChange verifies that presenting a valid
// token from a different device (changed fingerprint) returns 401 and
// revokes all tokens for the user.
func TestRefresh_DeviceFingerprintChange(t *testing.T) {
	svc, store := setupTestService(t)
	router := buildRouter(svc)

	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	seedToken(store, rawToken, 55, "original-fingerprint-hash", time.Now().Add(24*time.Hour))

	// Request has different headers -> different fingerprint.
	req := reqWithCookie("POST", "/auth/refresh-token", rawToken)
	req.Header.Set("User-Agent", "DifferentBrowser/1.0")
	req.Header.Set("Accept-Language", "fr")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	row, _ := store.GetByTokenHash(context.Background(), hashRaw(rawToken))
	require.NotNil(t, row)
	assert.True(t, row.Revoked, "token should be revoked on fingerprint mismatch")
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

// extractToken pulls the "token" value from a JSON body like
// {"token":"eyJ...","expires_at":"..."}.
func extractToken(t *testing.T, body string) string {
	t.Helper()
	// Minimal parse: find "token":" and grab until next "
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
