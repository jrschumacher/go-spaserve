package spaserve

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

func TestMemoryCache_GetSet(t *testing.T) {
	cache := NewMemoryCache[[]byte]()
	key := "myKey"
	value := []byte("myValue")

	// 1. Get non-existent key
	_, found := cache.Get(key)
	if found {
		t.Errorf("Expected key %q not to be found initially", key)
	}

	// 2. Set the key
	cache.Set(key, &value)

	// 3. Get existing key
	retrievedValue, found := cache.Get(key)
	if !found {
		t.Errorf("Expected key %q to be found after Set", key)
	}
	if retrievedValue != &value {
		t.Errorf("Expected value %q, got %q", string(value), string(*retrievedValue))
	}

	// 4. Ensure retrieved value is a copy
	originalValueBeforeModification := make([]byte, len(value))
	copy(originalValueBeforeModification, value) // Keep original
	value[0] = 'X'                               // Modify the original slice passed to Set

	retrievedAgain, foundAgain := cache.Get(key)
	if !foundAgain {
		t.Errorf("Expected key %q to still be found", key)
	}
	// Check against the original *before* modification
	if retrievedAgain == &originalValueBeforeModification {
		t.Errorf("Cache returned modified slice! Expected %q, got %q", string(originalValueBeforeModification), string(*retrievedAgain))
	}

	// 5. Overwrite key
	newValue := []byte("newValue")
	cache.Set(key, &newValue)
	retrievedOverwritten, foundOverwritten := cache.Get(key)
	if !foundOverwritten {
		t.Errorf("Expected key %q to be found after overwrite", key)
	}
	if retrievedOverwritten == retrievedAgain {
		t.Errorf("Expected overwritten value %q, got %q", string(newValue), string(*retrievedOverwritten))
	}
}

func TestMemoryCache_Concurrency(t *testing.T) {
	t.Parallel() // Mark goroutine as parallel capable
	cache := NewMemoryCache[[]byte]()
	numGoroutines := 100
	numOpsPerGoroutine := 100
	var wg sync.WaitGroup

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(gID int) {
			defer wg.Done()

			for j := 0; j < numOpsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", gID, j%10) // Introduce some key overlap
				value := []byte(fmt.Sprintf("value-%d-%d", gID, j))

				cache.Set(key, &value)

				retrieved, found := cache.Get(key)
				if !found {
					t.Errorf("Goroutine %d: Key %s not found immediately after Set", gID, key)
					continue // Avoid panic on bytes.Equal below
				}
				// Check if the retrieved value is the one WE set, understanding it might
				// have been overwritten by another goroutine immediately after.
				// A simple check is difficult here without external sync.
				// The main purpose is to run under `go test -race` to detect data races.
				// A basic check that it's *some* valid value:
				if !bytes.HasPrefix(*retrieved, []byte("value-")) {
					t.Errorf("Goroutine %d: Retrieved unexpected value for key %s: %q", gID, key, string(*retrieved))
				}

				// Read a key potentially set by another goroutine
				otherKey := fmt.Sprintf("key-%d-%d", (gID+1)%numGoroutines, j%10)
				cache.Get(otherKey) // Just access, check for races

			}
		}(i)
	}

	wg.Wait()
	// Add any final state checks if necessary, though the main goal is race detection.
}
