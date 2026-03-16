package cache

import (
	"sync"
	"time"
)

// Item holds a cached value with expiration time
type Item[T any] struct {
	Value      T
	ExpiresAt  time.Time
}

// IsExpired checks if the item has expired
func (i *Item[T]) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}

// Cache is a thread-safe in-memory cache with TTL support
type Cache[T any] struct {
	mu    sync.RWMutex
	items map[string]*Item[T]
}

// New creates a new cache
func New[T any]() *Cache[T] {
	return &Cache[T]{
		items: make(map[string]*Item[T]),
	}
}

// Set stores a value in the cache with the given TTL
func (c *Cache[T]) Set(key string, value T, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = &Item[T]{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Get retrieves a value from the cache if it exists and hasn't expired
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists || item.IsExpired() {
		var zero T
		return zero, false
	}

	return item.Value, true
}

// Delete removes a value from the cache
func (c *Cache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
}

// Clear removes all items from the cache
func (c *Cache[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*Item[T])
}

// Cleanup removes all expired items from the cache
func (c *Cache[T]) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, item := range c.items {
		if item.IsExpired() {
			delete(c.items, key)
		}
	}
}

// Count returns the number of items in the cache
func (c *Cache[T]) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.items)
}
