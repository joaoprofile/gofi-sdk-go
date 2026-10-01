package bucket

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// BucketInfo describes a bucket of the account.
type BucketInfo struct {
	Name      string
	Region    string    // empty when the provider does not report it
	CreatedAt time.Time // zero when the provider does not report it
}

// CreateOptions tunes CreateBucket.
type CreateOptions struct {
	Region string // empty uses Config.Region
}

// DeleteOptions tunes DeleteBucket.
type DeleteOptions struct {
	Force bool // delete every object first (DeletePrefix with "")
}

// Manager administers the buckets of an account. Safe for concurrent use.
//
// Every implementation follows the same rules: ListBuckets never returns a nil
// slice; CreateBucket validates the name (ErrInvalidBucketName) and reports
// ErrBucketExists for a taken name; DeleteBucket returns ErrBucketNotEmpty
// without Force and ErrBucketNotFound for a missing bucket; Open caches the
// Store per name and DeleteBucket evicts it.
type Manager interface {
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	CreateBucket(ctx context.Context, name string, opts CreateOptions) error
	DeleteBucket(ctx context.Context, name string, opts DeleteOptions) error
	// Open returns the Store of an existing bucket, configured for it
	// (e.g. its own region). It returns ErrBucketNotFound when missing.
	Open(ctx context.Context, name string) (Store, error)
}

// ManagerOpener builds a Manager for a provider from a generic Config.
type ManagerOpener func(ctx context.Context, cfg Config) (Manager, error)

var (
	managersMu sync.RWMutex
	managers   = map[Provider]ManagerOpener{}
)

// RegisterManager makes a provider's Manager available to OpenManager.
// Providers call it from the same init that calls Register.
func RegisterManager(p Provider, o ManagerOpener) {
	managersMu.Lock()
	defer managersMu.Unlock()
	managers[p] = o
}

// OpenManager builds the Manager for cfg.Provider; cfg.Name is ignored. It
// returns ErrNotSupported when the provider is imported but has no Manager.
func OpenManager(ctx context.Context, cfg Config) (Manager, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("%w: provider is not set", ErrInvalidConfig)
	}
	managersMu.RLock()
	o, ok := managers[cfg.Provider]
	managersMu.RUnlock()
	if ok {
		return o(ctx, cfg)
	}
	openersMu.RLock()
	_, registered := openers[cfg.Provider]
	openersMu.RUnlock()
	if registered {
		return nil, fmt.Errorf("%w: provider %q has no bucket manager", ErrNotSupported, cfg.Provider)
	}
	return nil, notRegistered(cfg.Provider)
}

// ValidateName checks name against the S3 bucket naming rules, the strictest
// common subset: 3 to 63 characters of [a-z0-9.-], starting and ending with a
// letter or digit, no "..", and not shaped like an IPv4 address. It returns
// ErrInvalidBucketName otherwise. Local providers reuse it so names stay
// portable.
func ValidateName(name string) error {
	invalid := func(why string) error {
		return fmt.Errorf("%w: %q %s", ErrInvalidBucketName, name, why)
	}
	if len(name) < 3 || len(name) > 63 {
		return invalid("must have 3 to 63 characters")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		alnum := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		switch {
		case (i == 0 || i == len(name)-1) && !alnum:
			return invalid("must start and end with a lowercase letter or digit")
		case !alnum && c != '.' && c != '-':
			return invalid("may only contain lowercase letters, digits, '.' and '-'")
		case c == '.' && name[i-1] == '.':
			return invalid("must not contain \"..\"")
		}
	}
	if addr, err := netip.ParseAddr(name); err == nil && addr.Is4() {
		return invalid("must not be an IP address")
	}
	return nil
}
