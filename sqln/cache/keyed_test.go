package cache

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyedEntriesAreIsolated(t *testing.T) {
	withRedis(t)
	ctx := context.Background()
	c := NewCache[int]("users", time.Minute)

	require.NoError(t, c.SetKeyed(ctx, "page-1", []int{1}))
	require.NoError(t, c.SetKeyed(ctx, "page-2", []int{2}))

	var got []int
	hit, err := c.GetKeyed(ctx, "page-2", &got)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, []int{2}, got)

	hit, err = c.GetKeyed(ctx, "page-3", &got)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestDelRemovesBaseAndKeyedEntries(t *testing.T) {
	mr := withRedis(t)
	ctx := context.Background()
	c := NewCache[int]("users", time.Minute)
	require.NoError(t, c.Set(ctx, []int{0}))
	require.NoError(t, c.SetKeyed(ctx, "page-1", []int{1}))

	require.NoError(t, c.Del(ctx))

	var got []int
	hit, _ := c.GetKeyed(ctx, "page-1", &got)
	assert.False(t, hit, "Del must invalidate keyed entries")
	list, _ := c.List(ctx)
	assert.Nil(t, list)
	for _, k := range mr.Keys() {
		assert.False(t, strings.Contains(k, "users"), "leftover key %s", k)
	}
}

func TestKeyedEntriesShareClusterSlot(t *testing.T) {
	c := NewCache[int]("users", time.Minute)
	assert.True(t, strings.HasPrefix(c.keyedKey("x"), "{"), "keyed keys need a hash tag for Redis Cluster")
	assert.Equal(t, hashTag(c.keyedKey("x")), hashTag(c.indexKey()))
}

func hashTag(k string) string { return k[:strings.IndexByte(k, '}')+1] }
