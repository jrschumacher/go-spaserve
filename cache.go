package spaserve

import "sync"

// MemoryCache provides a thread-safe in-memory cache using sync.Map.
type MemoryCache struct {
	store sync.Map
}

// NewMemoryCache creates a new empty MemoryCache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{}
}

// Get retrieves an item from the cache.
func (mc *MemoryCache) Get(key string) (data []byte, found bool) {
	value, ok := mc.store.Load(key)
	if !ok {
		return nil, false
	}
	// Assume stored value is []byte, otherwise panic (indicates misuse)
	data, ok = value.([]byte)
	if !ok {
		// This should not happen if Set only stores []byte
		panic("spaserve: MemoryCache stored non-[]byte value")
	}
	return data, true
}

// Set adds or updates an item in the cache.
func (mc *MemoryCache) Set(key string, data []byte) {
	// Store a copy to prevent external modification of the cached slice
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	mc.store.Store(key, dataCopy)
}

// Ensure MemoryCache implements the interface (compile-time check)
var _ Cache = (*MemoryCache)(nil)
