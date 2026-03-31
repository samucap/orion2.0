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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-minimum-length-for-security-1234"

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockRefreshTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*RefreshToken
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

func hashRaw(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func seedToken(store *mockRefreshTokenStore, raw string, userID int64, fingerprint string, expiresAt time.Time) {
	store.InsertRefreshToken(context.Background(), hashRaw(raw), userID, fingerprint, expiresAt)
}

// ---------------------------------------------------------------------------
// Service-layer tests
// ---------------------------------------------------------------------------

func TestCreateRefreshToken(t *testing.T) {
	svc, store := setupTestService(t)
	ctx := context.Background()

	raw, err := svc.CreateRefreshToken(ctx, 42, "fp-abc")
	require.NoError(t, err)
	assert.Len(t, raw, 64, "opaque token should be 64 hex chars (32 bytes)")

	hash := hashRaw(raw)
	row, err := store.GetByTokenHash(ctx, hash)
	require.NoError(t, err)
	require.NotNil(t, row, "token should be persisted in store")
	assert.Equal(t, int64(42), row.UserID)
	assert.Equal(t, "fp-abc", row.DeviceFingerprint)
	assert.False(t, row.Revoked)
}

func TestValidateAndRotate_Success(t *testing.T) {
	svc, store := setupTestService(t)
	ctx := context.Background()

	rawOld := "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344"
	fp := "stable-fingerprint"
	seedToken(store, rawOld, 42, fp, time.Now().Add(24*time.Hour))

	pair, err := svc.ValidateAndRotate(ctx, rawOld, fp)
	require.NoError(t, err)
	require.NotNil(t, pair)

	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)
	assert.NotEqual(t, rawOld, pair.RefreshToken, "rotated token must differ")

	oldRow, _ := store.GetByTokenHash(ctx, hashRaw(rawOld))
	require.NotNil(t, oldRow)
	assert.True(t, oldRow.Revoked, "old token should be revoked after rotation")
}

func TestValidateAndRotate_RevokedReplay(t *testing.T) {
	svc, store := setupTestService(t)
	ctx := context.Background()

	rawOld := "deadbeef00000000deadbeef00000000deadbeef00000000deadbeef00000000"
	fp := "stable-fingerprint"
	seedToken(store, rawOld, 99, fp, time.Now().Add(24*time.Hour))

	store.mu.Lock()
	store.tokens[hashRaw(rawOld)].Revoked = true
	store.mu.Unlock()

	rawSibling := "11111111222222223333333344444444aaaabbbbccccddddeeee000011112222"
	seedToken(store, rawSibling, 99, fp, time.Now().Add(24*time.Hour))

	_, err := svc.ValidateAndRotate(ctx, rawOld, fp)
	assert.ErrorIs(t, err, ErrTokenRevoked)

	sibling, _ := store.GetByTokenHash(ctx, hashRaw(rawSibling))
	require.NotNil(t, sibling)
	assert.True(t, sibling.Revoked, "sibling token should be revoked after replay detection")
}

func TestValidateAndRotate_Expired(t *testing.T) {
	svc, store := setupTestService(t)
	ctx := context.Background()

	rawExpired := "eeeeeeee00000000eeeeeeee00000000eeeeeeee00000000eeeeeeee00000000"
	fp := "stable-fingerprint"
	seedToken(store, rawExpired, 7, fp, time.Now().Add(-1*time.Hour))

	_, err := svc.ValidateAndRotate(ctx, rawExpired, fp)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidateAndRotate_FingerprintMismatch(t *testing.T) {
	svc, store := setupTestService(t)
	ctx := context.Background()

	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	seedToken(store, rawToken, 55, "original-fingerprint", time.Now().Add(24*time.Hour))

	_, err := svc.ValidateAndRotate(ctx, rawToken, "different-fingerprint")
	assert.ErrorIs(t, err, ErrFingerprintMismatch)

	row, _ := store.GetByTokenHash(ctx, hashRaw(rawToken))
	require.NotNil(t, row)
	assert.True(t, row.Revoked, "token should be revoked on fingerprint mismatch")
}

func TestRevokeRefreshToken(t *testing.T) {
	svc, store := setupTestService(t)
	ctx := context.Background()

	raw := "cccccccc00000000cccccccc00000000cccccccc00000000cccccccc00000000"
	seedToken(store, raw, 10, "fp", time.Now().Add(24*time.Hour))

	err := svc.RevokeRefreshToken(ctx, raw)
	require.NoError(t, err)

	row, _ := store.GetByTokenHash(ctx, hashRaw(raw))
	require.NotNil(t, row)
	assert.True(t, row.Revoked)
}

func TestComputeDeviceFingerprint(t *testing.T) {
	svc := &RefreshService{}

	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1.Header.Set("User-Agent", "TestAgent")
	r1.Header.Set("Accept-Language", "en")

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("User-Agent", "TestAgent")
	r2.Header.Set("Accept-Language", "en")

	assert.Equal(t, svc.ComputeDeviceFingerprint(r1), svc.ComputeDeviceFingerprint(r2),
		"same headers should produce the same fingerprint")

	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.Header.Set("User-Agent", "DifferentBrowser")
	r3.Header.Set("Accept-Language", "fr")

	assert.NotEqual(t, svc.ComputeDeviceFingerprint(r1), svc.ComputeDeviceFingerprint(r3),
		"different headers should produce different fingerprints")
}
