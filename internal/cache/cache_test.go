package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/samucap/orion2.0/internal/cache"
	"github.com/stretchr/testify/assert"
)

func TestInMemoryCacheSetAndGet(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	// Test setting and getting a value
	key := "test-key"
	value := []byte("test-value")

	c.Set(ctx, key, value, time.Minute)

	retrieved, found := c.Get(ctx, key)
	assert.True(t, found)
	assert.Equal(t, value, retrieved)
}

func TestInMemoryCacheGetNonExistent(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	retrieved, found := c.Get(ctx, "non-existent-key")
	assert.False(t, found)
	assert.Nil(t, retrieved)
}

func TestInMemoryCacheTTLExpiration(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	key := "ttl-test"
	value := []byte("expires-soon")

	// Set with very short TTL
	c.Set(ctx, key, value, 10*time.Millisecond)

	// Should exist immediately
	retrieved, found := c.Get(ctx, key)
	assert.True(t, found)
	assert.Equal(t, value, retrieved)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should be expired and cleaned up
	retrieved, found = c.Get(ctx, key)
	assert.False(t, found)
	assert.Nil(t, retrieved)
}

func TestInMemoryCacheDelete(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	key := "delete-test"
	value := []byte("to-be-deleted")

	c.Set(ctx, key, value, time.Minute)

	// Verify it exists
	retrieved, found := c.Get(ctx, key)
	assert.True(t, found)
	assert.Equal(t, value, retrieved)

	// Delete it
	c.Delete(ctx, key)

	// Verify it's gone
	retrieved, found = c.Get(ctx, key)
	assert.False(t, found)
	assert.Nil(t, retrieved)
}

func TestInMemoryCacheUpdateExisting(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	key := "update-test"
	oldValue := []byte("old-value")
	newValue := []byte("new-value")

	// Set initial value
	c.Set(ctx, key, oldValue, time.Minute)

	// Update with new value
	c.Set(ctx, key, newValue, time.Minute)

	// Should return new value
	retrieved, found := c.Get(ctx, key)
	assert.True(t, found)
	assert.Equal(t, newValue, retrieved)
}

func TestInMemoryCacheCleanupExpired(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	// Set two items, one expires soon
	c.Set(ctx, "long-lived", []byte("stays"), time.Minute)
	c.Set(ctx, "short-lived", []byte("expires"), 10*time.Millisecond)

	// Both should exist initially
	longVal, longFound := c.Get(ctx, "long-lived")
	assert.True(t, longFound)
	assert.Equal(t, []byte("stays"), longVal)

	shortVal, shortFound := c.Get(ctx, "short-lived")
	assert.True(t, shortFound)
	assert.Equal(t, []byte("expires"), shortVal)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Manual cleanup (normally done automatically on Get)
	c.CleanupExpired()

	// Long-lived should still exist
	longVal, longFound = c.Get(ctx, "long-lived")
	assert.True(t, longFound)
	assert.Equal(t, []byte("stays"), longVal)

	// Short-lived should be gone
	shortVal, shortFound = c.Get(ctx, "short-lived")
	assert.False(t, shortFound)
	assert.Nil(t, shortVal)
}

func TestInMemoryCacheLargeValues(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	// Test with a larger value (1KB)
	largeValue := make([]byte, 1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	key := "large-test"
	c.Set(ctx, key, largeValue, time.Minute)

	retrieved, found := c.Get(ctx, key)
	assert.True(t, found)
	assert.Equal(t, largeValue, retrieved)
}

func TestInMemoryCacheClear(t *testing.T) {
	ctx := context.Background()
	c := cache.NewInMemoryCache()

	// Set multiple items
	keys := []string{"key1", "key2", "key3"}
	values := [][]byte{[]byte("val1"), []byte("val2"), []byte("val3")}

	for i, key := range keys {
		c.Set(ctx, key, values[i], time.Minute)
	}

	// Verify all exist
	for i, key := range keys {
		retrieved, found := c.Get(ctx, key)
		assert.True(t, found)
		assert.Equal(t, values[i], retrieved)
	}

	// Clear the cache
	c.Clear(ctx)

	// Verify all are gone
	for _, key := range keys {
		retrieved, found := c.Get(ctx, key)
		assert.False(t, found)
		assert.Nil(t, retrieved)
	}
}
