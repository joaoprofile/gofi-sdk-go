package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DefaultSessionConfig ---

func TestDefaultSessionConfig(t *testing.T) {
	cfg := DefaultSessionConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, 10*time.Minute, cfg.TTL)
	assert.Equal(t, "session", cfg.Prefix)
}

// --- Entry.IsExpired ---

func TestEntry_IsExpired_False(t *testing.T) {
	e := &Entry{ExpiresAt: time.Now().Add(time.Minute)}
	assert.False(t, e.IsExpired())
}

func TestEntry_IsExpired_True(t *testing.T) {
	e := &Entry{ExpiresAt: time.Now().Add(-time.Second)}
	assert.True(t, e.IsExpired())
}

func TestEntry_IsExpired_JustPast(t *testing.T) {
	e := &Entry{ExpiresAt: time.Now().Add(-time.Nanosecond)}
	assert.True(t, e.IsExpired())
}

// --- Entry.TTL ---

func TestEntry_TTL_Positive(t *testing.T) {
	d := 5 * time.Minute
	e := &Entry{ExpiresAt: time.Now().Add(d)}
	assert.True(t, e.TTL() > 0)
	assert.True(t, e.TTL() <= d)
}

func TestEntry_TTL_NegativeWhenExpired(t *testing.T) {
	e := &Entry{ExpiresAt: time.Now().Add(-time.Second)}
	assert.True(t, e.TTL() < 0)
}

// --- Entry.Extend ---

func TestEntry_Extend_PushesExpiry(t *testing.T) {
	e := &Entry{ExpiresAt: time.Now().Add(time.Minute)}
	before := e.ExpiresAt
	e.Extend(10 * time.Minute)
	assert.True(t, e.ExpiresAt.After(before))
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), e.ExpiresAt, time.Second)
}

func TestEntry_Extend_ReactivatesExpired(t *testing.T) {
	e := &Entry{ExpiresAt: time.Now().Add(-time.Second)}
	assert.True(t, e.IsExpired())
	e.Extend(time.Minute)
	assert.False(t, e.IsExpired())
}

// --- Entry.Get / Set ---

func TestEntry_SetAndGet(t *testing.T) {
	e := &Entry{}
	e.Set("role", "admin")
	assert.Equal(t, "admin", e.Get("role"))
}

func TestEntry_Get_MissingKey(t *testing.T) {
	e := &Entry{Data: SessionData{"a": 1}}
	assert.Nil(t, e.Get("missing"))
}

func TestEntry_Get_NilData(t *testing.T) {
	e := &Entry{}
	assert.Nil(t, e.Get("anything"))
}

func TestEntry_Set_InitializesNilData(t *testing.T) {
	e := &Entry{}
	e.Set("k", "v")
	require.NotNil(t, e.Data)
	assert.Equal(t, "v", e.Data["k"])
}

func TestEntry_Set_Chainable(t *testing.T) {
	e := &Entry{}
	result := e.Set("a", 1).Set("b", 2).Set("c", 3)
	assert.Equal(t, e, result)
	assert.Equal(t, 1, e.Get("a"))
	assert.Equal(t, 2, e.Get("b"))
	assert.Equal(t, 3, e.Get("c"))
}

func TestEntry_Set_OverwritesValue(t *testing.T) {
	e := &Entry{Data: SessionData{"x": "old"}}
	e.Set("x", "new")
	assert.Equal(t, "new", e.Get("x"))
}

// --- Constants / Errors ---

func TestLockStates_Distinct(t *testing.T) {
	states := []string{StateProcessing, StateDone, StateFailed}
	seen := map[string]struct{}{}
	for _, s := range states {
		_, dup := seen[s]
		assert.False(t, dup, "duplicate state: %s", s)
		seen[s] = struct{}{}
	}
}

func TestErrors_Distinct(t *testing.T) {
	errs := []error{ErrAlreadyLocked, ErrNotLocked, ErrInvalidKey, ErrEncodingFailed}
	seen := map[string]struct{}{}
	for _, e := range errs {
		_, dup := seen[e.Error()]
		assert.False(t, dup, "duplicate error message: %s", e)
		seen[e.Error()] = struct{}{}
	}
}
