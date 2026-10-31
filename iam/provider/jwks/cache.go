package jwks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/internal/flight"
)

// MaxBody caps a JWKS response.
const MaxBody = 1 << 20

// DefaultCacheTTL is how long a fetched key set is served before a refetch.
const DefaultCacheTTL = time.Hour

// minRefresh bounds refetches for unknown kids, so tokens with random kids
// cannot turn every verification into a JWKS request.
const minRefresh = 30 * time.Second

// ErrInsecureEndpoint is returned for an endpoint that is not https (plain
// http is accepted only on loopback, for tests).
var ErrInsecureEndpoint = errors.New("iam: endpoint must use https")

// ErrKeyNotFound is returned when no key in the set matches the kid.
var ErrKeyNotFound = errors.New("iam/jwks: key not found")

// RequireHTTPS accepts https, and http only for loopback hosts.
func RequireHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("iam/jwks: invalid endpoint %q: %w", raw, err)
	}
	if u.Scheme == "https" && u.Host != "" {
		return nil
	}
	if u.Scheme == "http" && isLoopback(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInsecureEndpoint, raw)
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type cachedKey struct {
	Kid string
	Key any // *rsa.PublicKey or *ecdsa.PublicKey
}

// Cache holds the keys of one JWKS URL. Concurrent refetches share one
// request, and a failed refetch keeps the previous keys in use.
type Cache struct {
	ttl       time.Duration
	mu        sync.RWMutex
	keys      []cachedKey
	fetchedAt time.Time
	flight    flight.Group
}

// NewCache returns a cache that refetches after ttl; ttl <= 0 uses DefaultCacheTTL.
func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &Cache{ttl: ttl}
}

// GetKey returns the key for kid (an empty kid matches the first key),
// fetching the set when it is stale or the kid is unknown.
func (c *Cache) GetKey(ctx context.Context, client *http.Client, jwksURL, kid string) (any, error) {
	key, age := c.lookup(kid)
	fresh := age < c.ttl
	if key != nil && fresh {
		return key, nil
	}
	if fresh && age < minRefresh {
		return nil, fmt.Errorf("%w: %q", ErrKeyNotFound, kid)
	}

	// Refetch; on failure the previous keys stay in use.
	err := c.flight.Do(ctx, func(fctx context.Context) error { return c.fetch(fctx, client, jwksURL) })
	if key, _ = c.lookup(kid); key != nil {
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: %q", ErrKeyNotFound, kid)
}

// Refresh fetches the set now and reports how many usable keys it has.
func (c *Cache) Refresh(ctx context.Context, client *http.Client, jwksURL string) (int, error) {
	if err := c.flight.Do(ctx, func(fctx context.Context) error { return c.fetch(fctx, client, jwksURL) }); err != nil {
		return 0, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.keys), nil
}

// lookup returns the key for kid and the age of the cached set.
func (c *Cache) lookup(kid string) (any, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.findKey(kid), time.Since(c.fetchedAt)
}

func (c *Cache) findKey(kid string) any {
	for _, k := range c.keys {
		if kid == "" || k.Kid == kid {
			return k.Key
		}
	}
	return nil
}

// fetch replaces the cached keys only with a 200 response that yields at
// least one usable key; any failure keeps the previous keys.
func (c *Cache) fetch(ctx context.Context, client *http.Client, jwksURL string) error {
	if err := RequireHTTPS(jwksURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("iam/jwks: fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iam/jwks: returned status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwkKey `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxBody)).Decode(&doc); err != nil {
		return fmt.Errorf("iam/jwks: decode failed: %w", err)
	}

	var parsed []cachedKey
	for _, k := range doc.Keys {
		pub, err := k.PublicKey()
		if err != nil {
			continue // skip malformed and symmetric keys
		}
		parsed = append(parsed, cachedKey{Kid: k.Kid, Key: pub})
	}
	if len(parsed) == 0 {
		return errors.New("iam/jwks: no usable keys")
	}

	c.mu.Lock()
	c.keys = parsed
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	return nil
}
