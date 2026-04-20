package session

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- GenerateSessionID ---

func TestGenerateSessionID_Format(t *testing.T) {
	id := GenerateSessionID()
	assert.True(t, strings.HasPrefix(id, "session_"), "must start with 'session_'")

	parts := strings.Split(id, "_")
	assert.Equal(t, 4, len(parts), "must have 4 underscore-separated parts")
}

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	const n = 1000
	ids := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := GenerateSessionID()
		_, exists := ids[id]
		assert.False(t, exists, "duplicate session ID generated: %s", id)
		ids[id] = struct{}{}
	}
}

func TestGenerateSessionID_ConcurrentUniqueness(t *testing.T) {
	const n = 500
	mu := sync.Mutex{}
	ids := make(map[string]struct{}, n)
	var wg sync.WaitGroup

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			id := GenerateSessionID()
			mu.Lock()
			ids[id] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	assert.Equal(t, n, len(ids), "all concurrent IDs must be unique")
}

// --- NewKey ---

func TestNewKey_Consistency(t *testing.T) {
	key1 := NewKey("lock", "sku", "ua123")
	key2 := NewKey("lock", "sku", "ua123")
	assert.Equal(t, key1, key2, "same inputs must produce the same key")
}

func TestNewKey_DifferentInputs(t *testing.T) {
	key1 := NewKey("lock", "sku", "ua123")
	key2 := NewKey("lock", "ean", "1712112456521")
	assert.NotEqual(t, key1, key2)
}

func TestNewKey_PrefixAndLength(t *testing.T) {
	key := NewKey("lock", "sku_marketplace", "MLB12345612222")

	assert.True(t, strings.HasPrefix(key, "lock:"), "must start with 'lock:'")
	// prefix + ":" + 16 hex chars
	assert.Equal(t, len("lock:")+16, len(key))
}

func TestNewKey_DifferentPrefixes(t *testing.T) {
	k1 := NewKey("lock", "sku", "123")
	k2 := NewKey("session", "sku", "123")

	assert.NotEqual(t, k1, k2)
	assert.True(t, strings.HasPrefix(k1, "lock:"))
	assert.True(t, strings.HasPrefix(k2, "session:"))
}

func TestNewKey_NoParts(t *testing.T) {
	key := NewKey("lock")
	assert.True(t, strings.HasPrefix(key, "lock:"))
	assert.Equal(t, len("lock:")+16, len(key))
}

func TestNewKey_OrderSensitive(t *testing.T) {
	k1 := NewKey("lock", "a", "b")
	k2 := NewKey("lock", "b", "a")
	assert.NotEqual(t, k1, k2, "key must depend on part order")
}
