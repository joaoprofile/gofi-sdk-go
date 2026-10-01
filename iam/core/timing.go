package core

import (
	"context"
	"sync"

	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/password"
)

// defaultDummyHash is an Argon2id hash with password.DefaultParams, the
// parameters the SDK recommends for stored hashes.
var defaultDummyHash = sync.OnceValue(func() string {
	h, _ := password.DefaultParams.Hash("gofi-timing-equalizer")
	return h
})

// dummyHash is the hash verified for unknown emails: SecurityConfig.DummyPasswordHash
// when set (same algorithm and cost as the stored hashes), the Argon2id default otherwise.
func (c AuthConfig) dummyHash() string {
	if c.dummyPasswordHash != "" {
		return c.dummyPasswordHash
	}
	return defaultDummyHash()
}

// dummyPasswordCheck burns the cost of a real verification when the user does
// not exist, so response time does not reveal which emails are registered.
func (a *localAuth) dummyPasswordCheck(ctx context.Context, pw string) {
	if v, ok := a.user.(port.DummyPasswordVerifier); ok {
		v.DummyValidatePassword(ctx, pw)
		return
	}
	_ = password.Verify(a.cfg.dummyHash(), pw)
}
