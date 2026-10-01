package memory

import (
	"context"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

func TestSave_DoesNotStoreAccessToken(t *testing.T) {
	p := NewProvider()
	defer p.Stop()
	_ = p.Save(context.Background(), &types.Session{ID: "s1", UserID: "u1", AccessToken: "at", ExpiresAt: time.Now().Add(time.Hour)})
	got, err := p.Get(context.Background(), "s1")
	if err != nil || got.AccessToken != "" {
		t.Fatalf("access token must not be stored: %+v %v", got, err)
	}
}
