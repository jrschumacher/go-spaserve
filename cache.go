package spaserve

import "sync"

// MemoryCache provides a thread-safe in-memory cache using sync.Map.
type MemoryCache[T any] struct {
	store sync.Map
}

// NewMemoryCache creates a new empty MemoryCache.
func NewMemoryCache[T any]() *MemoryCache[T] {
	return &MemoryCache[T]{}
}

// Get retrieves an item from the cache.
func (mc *MemoryCache[T]) Get(key string) (data *T, found bool) {
	value, ok := mc.store.Load(key)
	if !ok {
		return nil, false
	}
	// Assume stored value is T, otherwise panic (indicates misuse)
	data, ok = value.(*T)
	if !ok {
		// This should not happen if Set only stores T
		panic("spaserve: MemoryCache stored non-T value")
	}
	return data, true
}

// Set adds or updates an item in the cache.
func (mc *MemoryCache[T]) Set(key string, data *T) {
	mc.store.Store(key, data)
}

// Ensure MemoryCache implements the interface (compile-time check)
var _ Cache[[]byte] = (*MemoryCache[[]byte])(nil)
