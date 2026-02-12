package cache

import "sync"

// Cache stores values keyed by string.
type Cache[T any] struct {
	mu    sync.Mutex
	items map[string]T
}

// New creates a new Cache instance.
func New[T any]() *Cache[T] {
	return &Cache[T]{
		items: make(map[string]T),
	}
}

// Get returns a cached value and whether it exists.
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value, ok := c.items[key]
	return value, ok
}

// Set stores a value in the cache.
func (c *Cache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = value
}
