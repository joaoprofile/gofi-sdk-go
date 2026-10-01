package memory

import (
	"context"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
)

var _ port.SessionRevoker = (*Provider)(nil)

func TestRevokeIfActive(t *testing.T) {
	p := NewProvider()
	defer p.Stop()
	ctx := context.Background()
	_ = p.Save(ctx, &types.Session{ID: "s1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})

	if ok, err := p.RevokeIfActive(ctx, "s1"); !ok || err != nil {
		t.Fatalf("first revoke: ok=%v err=%v", ok, err)
	}
	if ok, err := p.RevokeIfActive(ctx, "s1"); ok || err != nil {
		t.Fatalf("second revoke: ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestRevoke_KeepsHashAndAuthTimeForReuseDetection(t *testing.T) {
	p := NewTestProvider()
	ctx := context.Background()
	auth := time.Now().Add(-time.Hour)
	_ = p.Save(ctx, &types.Session{ID: "s1", UserID: "u1", RefreshTokenHash: "h", AuthTime: auth, ExpiresAt: time.Now().Add(time.Hour)})
	_ = p.Revoke(ctx, "s1")
	s, err := p.Get(ctx, "s1")
	if err != nil || !s.Revoked || s.RefreshTokenHash != "h" || !s.AuthTime.Equal(auth) {
		t.Fatalf("revoked record lost fields: %v %+v", err, s)
	}
}
