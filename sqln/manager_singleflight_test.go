package sqln

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofi-labs/gofi-sdk-go/sqln/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Concurrent misses of the same cached query reach the database once.
func TestCachedQuery_StampedeRunsOneQuery(t *testing.T) {
	mr := miniredis.RunT(t)
	cache.UseClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	t.Cleanup(func() { cache.UseClient(nil) })
	setupGlobal(t, "slow-rows")
	resetRecordedQueries(t)
	c := cache.NewCache[mappedItem]("stampede", time.Minute)

	const callers = 20
	results := make([][]mappedItem, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			list, err := Find[mappedItem](context.Background(), "SELECT id, name FROM t").WithCache(c).List()
			require.NoError(t, err)
			results[i] = list
		})
	}
	wg.Wait()

	assert.Len(t, recorded(t), 1, "one database query for %d concurrent misses", callers)
	results[0][0].Name = "mutated"
	for i := 1; i < callers; i++ {
		assert.Equal(t, "a", results[i][0].Name, "callers must not share the result slice")
	}
}
