package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/joaoprofile/gofi-sdk-go/netx"
)

const sessionCookie = "sid"

var errUnauthenticated = errors.New("not authenticated")

type claimsKey struct{}

// claimsFrom returns the claims the auth middleware put in the context.
func claimsFrom(ctx context.Context) *types.Claims {
	c, _ := ctx.Value(claimsKey{}).(*types.Claims)
	return c
}

// authenticator guards private routes. It accepts either credential:
//
//	Authorization: Bearer <jwt>   token mode: the client holds the JWT
//	Cookie: sid=<random id>       session mode: the JWT stays on the server
//
// Both end in iam.ValidateToken, which checks the signature and also that the
// session was not revoked: logout works at once in both modes.
type authenticator struct {
	iam   *core.IAMService
	vault *vault
}

func (a *authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var claims *types.Claims
		err := errUnauthenticated
		if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			claims, err = a.iam.ValidateToken(r.Context(), token)
		} else if c, cerr := r.Cookie(sessionCookie); cerr == nil {
			claims, err = a.fromSession(r.Context(), c.Value)
		}
		if err != nil {
			netx.Error(w, http.StatusUnauthorized, errUnauthenticated)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
	})
}

// fromSession validates the JWT kept for this cookie. When it has expired,
// it is renewed with the stored refresh token; the browser never notices.
func (a *authenticator) fromSession(ctx context.Context, id string) (*types.Claims, error) {
	s, ok := a.vault.get(id)
	if !ok {
		return nil, errUnauthenticated
	}
	claims, err := a.iam.ValidateToken(ctx, s.AccessToken)
	if !errors.Is(err, core.ErrTokenExpired) {
		return claims, err
	}
	s, err = a.vault.renew(id, s, func(old *types.Session) (*types.Session, error) {
		return a.iam.RefreshToken(ctx, old.RefreshToken)
	})
	if err != nil {
		return nil, err
	}
	return a.iam.ValidateToken(ctx, s.AccessToken)
}

// vault keeps the tokens of each browser session on the server, under a
// random id sent as an HttpOnly cookie. JavaScript never sees a token, so an
// XSS cannot steal one. With several instances, keep it in Redis.
type vault struct {
	mu sync.Mutex
	m  map[string]*types.Session
}

func newVault() *vault { return &vault{m: map[string]*types.Session{}} }

func (v *vault) put(s *types.Session) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	v.mu.Lock()
	v.m[id] = s
	v.mu.Unlock()
	return id
}

func (v *vault) get(id string) (*types.Session, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.m[id]
	return s, ok
}

func (v *vault) delete(id string) {
	v.mu.Lock()
	delete(v.m, id)
	v.mu.Unlock()
}

// renew replaces the tokens of id once. Concurrent requests holding the same
// expired token wait here and reuse the result: using a refresh token twice
// looks like token theft to iam, which then revokes every session of the user.
func (v *vault) renew(id string, seen *types.Session, refresh func(*types.Session) (*types.Session, error)) (*types.Session, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, ok := v.m[id]
	if !ok {
		return nil, errUnauthenticated
	}
	if cur != seen {
		return cur, nil // another request already renewed it
	}
	next, err := refresh(cur)
	if err != nil {
		delete(v.m, id)
		return nil, err
	}
	v.m[id] = next
	return next, nil
}
