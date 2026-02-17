package cache

import (
	"context"
	"sync"
	"time"
)

// Cache defines the interface for caching operations
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration)
	Delete(ctx context.Context, key string)
	Clear(ctx context.Context)
}

// InMemoryCache implements an in-memory cache with TTL support
type InMemoryCache struct {
	data sync.Map
}

// cacheItem represents a cached item with expiration
type cacheItem struct {
	value     []byte
	expiresAt time.Time
}

// NewInMemoryCache creates a new in-memory cache instance
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{}
}

// Get retrieves a value from the cache if it exists and hasn't expired
func (c *InMemoryCache) Get(ctx context.Context, key string) ([]byte, bool) {
	value, found := c.data.Load(key)
	if !found {
		return nil, false
	}

	item, ok := value.(*cacheItem)
	if !ok {
		return nil, false
	}

	// Check if item has expired
	if time.Now().After(item.expiresAt) {
		// Remove expired item
		c.data.Delete(key)
		return nil, false
	}

	return item.value, true
}

// Set stores a value in the cache with the specified TTL
func (c *InMemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	item := &cacheItem{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	c.data.Store(key, item)
}

// Delete removes a key from the cache
func (c *InMemoryCache) Delete(ctx context.Context, key string) {
	c.data.Delete(key)
}

// Clear removes all items from the cache
func (c *InMemoryCache) Clear(ctx context.Context) {
	c.data = sync.Map{}
}

// CleanupExpired removes all expired items from the cache
// This is an optional maintenance method that can be called periodically
func (c *InMemoryCache) CleanupExpired() {
	c.data.Range(func(key, value interface{}) bool {
		if item, ok := value.(*cacheItem); ok {
			if time.Now().After(item.expiresAt) {
				c.data.Delete(key)
			}
		}
		return true
	})
}
