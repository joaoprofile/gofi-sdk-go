package sqln

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"log/slog"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"
	"weak"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"golang.org/x/sync/singleflight"
)

type cacheScopeKey struct{}

// ContextWithCacheScope returns ctx carrying a cache scope that every cached
// query run with it mixes into its key (see WithCacheScope). Set it once per
// request, e.g. in the middleware that selects the tenant.
func ContextWithCacheScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, cacheScopeKey{}, scope)
}

// flights collapses concurrent cache misses for the same query into one
// database round trip (cache stampede protection).
var flights singleflight.Group

// cached serves kind from the cache, or fetches it once among concurrent
// callers with the same key and stores it. Without a usable key it just fetches.
func cached[T, R any](q *manager[T], instance *sql.DB, kind string, fetch func(*sql.DB) (R, error), clone func(R) R) (R, error) {
	key, ok := q.cacheKey(instance, kind)
	if !ok {
		return fetch(instance)
	}
	var hit R
	if found, _ := q.cache.GetKeyed(q.ctx, key, &hit); found {
		return hit, nil
	}
	// The cache pointer keeps callers of distinct caches from skipping each other's store.
	return dedup(fmt.Sprintf("%s|%p", key, q.cache), func() (R, error) {
		r, err := fetch(instance)
		if err == nil && !isNilPointer(r) {
			q.storeInCache(key, r)
		}
		return r, err
	}, clone)
}

// dedup runs fetch once per key among concurrent callers. Followers get a
// copy so no two callers share a mutable result; they also share the
// leader's outcome, including a cancellation of the leader's context.
func dedup[R any](key string, fetch func() (R, error), clone func(R) R) (R, error) {
	v, err, shared := flights.Do(key, func() (any, error) { return fetch() })
	r, _ := v.(R)
	if err != nil || !shared {
		return r, err
	}
	return clone(r), nil
}

func isNilPointer(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

func clonePage[T any](p *Page[T]) *Page[T] {
	if p == nil {
		return nil
	}
	c := *p
	c.Content = slices.Clone(p.Content)
	return &c
}

// storeInCache is best-effort: a cache failure never fails the query.
func (q *manager[T]) storeInCache(key string, data any) {
	if err := q.cache.SetKeyed(q.ctx, key, data); err != nil {
		slog.WarnContext(q.ctx, "sqln: cache write failed", slog.Any("error", err))
	}
}

// cacheKey scopes a cached result to the database, cache scope, result type,
// kind, query, arguments and page. ok is false when the result must not be
// cached: no cache, a transaction in ctx (uncommitted or session-bound rows),
// or an argument without a deterministic encoding.
func (q *manager[T]) cacheKey(instance *sql.DB, kind string) (key string, ok bool) {
	if q.cache == nil || instance == nil {
		return "", false
	}
	if _, inTx := connection.TxFor(q.ctx, instance); inTx {
		return "", false
	}
	h := sha256.New()
	ctxScope, _ := q.ctx.Value(cacheScopeKey{}).(string)
	for _, s := range []string{q.databaseID(instance), q.cacheScope, ctxScope, reflect.TypeFor[T]().String(), kind, q.query} {
		writeBytes(h, 's', []byte(s))
	}
	if !writeArgs(h, q.args) {
		return "", false
	}
	if q.page != nil {
		writeBytes(h, 'p', binary.BigEndian.AppendUint32(nil, uint32(q.page.Page)<<16|uint32(q.page.Limit)))
		for _, o := range q.page.Order {
			writeBytes(h, 's', []byte(o.Field))
			writeBytes(h, 's', []byte(o.Direction))
		}
		writeBytes(h, 's', []byte(q.countQuery))
		if !writeArgs(h, q.countArgs) {
			return "", false
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:16]), true
}

// databaseID is the connection ID when instance belongs to it, else an
// identity of the pool itself valid within this process only.
func (q *manager[T]) databaseID(instance *sql.DB) string {
	conn := q.conn
	if conn == nil {
		conn, _ = connection.Global()
	}
	if conn != nil && conn.ID() != "" && (instance == conn.DB() || instance == conn.ReadDB()) {
		return conn.ID()
	}
	return poolID(instance)
}

var (
	poolIDs     sync.Map // weak.Pointer[sql.DB] -> string
	processSalt = rand.Text()
)

// poolID gives each live pool a unique ID. A weak pointer, unlike an address,
// never matches a later pool allocated at the same address after this one is freed.
func poolID(db *sql.DB) string {
	wp := weak.Make(db)
	if id, ok := poolIDs.Load(wp); ok {
		return id.(string)
	}
	id, loaded := poolIDs.LoadOrStore(wp, "pool:"+processSalt+":"+rand.Text())
	if !loaded {
		runtime.AddCleanup(db, func(k weak.Pointer[sql.DB]) { poolIDs.Delete(k) }, wp)
	}
	return id.(string)
}

// writeBytes writes a tagged, length-prefixed field so no two field sequences
// share an encoding.
func writeBytes(h hash.Hash, tag byte, b []byte) {
	var hdr [9]byte
	hdr[0] = tag
	binary.BigEndian.PutUint64(hdr[1:], uint64(len(b)))
	h.Write(hdr[:])
	h.Write(b)
}

func writeArgs(h hash.Hash, args []any) bool {
	writeBytes(h, 'n', binary.BigEndian.AppendUint64(nil, uint64(len(args))))
	for _, a := range args {
		if !writeArg(h, a) {
			return false
		}
	}
	return true
}

// writeArg encodes a by the value the driver receives (Valuer and named basic
// types resolved), never by fmt, whose String methods may mask the value.
// Slices, which some drivers bind as arrays, are encoded element by element.
func writeArg(h hash.Hash, a any) bool {
	v, err := driver.DefaultParameterConverter.ConvertValue(a)
	if err != nil {
		rv := reflect.ValueOf(a)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return false
		}
		writeBytes(h, 'L', []byte(rv.Type().String()))
		writeBytes(h, 'n', binary.BigEndian.AppendUint64(nil, uint64(rv.Len()))) // #nosec G115 -- lengths are non-negative
		for i := range rv.Len() {
			if !writeArg(h, rv.Index(i).Interface()) {
				return false
			}
		}
		return true
	}
	switch v := v.(type) {
	case nil:
		writeBytes(h, '0', nil)
	case int64:
		writeBytes(h, 'i', binary.BigEndian.AppendUint64(nil, uint64(v))) // #nosec G115 -- two's complement bits are the intended encoding
	case uint64:
		writeBytes(h, 'u', binary.BigEndian.AppendUint64(nil, v))
	case float64:
		writeBytes(h, 'f', binary.BigEndian.AppendUint64(nil, math.Float64bits(v)))
	case bool:
		writeBytes(h, 'b', []byte(strconv.FormatBool(v)))
	case []byte:
		writeBytes(h, 'y', v)
	case string:
		writeBytes(h, 's', []byte(v))
	case time.Time:
		writeBytes(h, 't', []byte(v.Format(time.RFC3339Nano)))
	default:
		return false
	}
	return true
}
