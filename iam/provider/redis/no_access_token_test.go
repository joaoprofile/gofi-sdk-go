package redis

import (
	"context"
	"testing"
	"time"
)

func TestSave_DoesNotPersistAccessToken(t *testing.T) {
	p, _ := newTestProvider(t)
	s := newTestSession("s1", "u1", time.Hour)
	if err := p.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "" || got.RefreshToken != "" {
		t.Fatalf("bearer tokens must not be persisted: %+v", got)
	}
	if s.AccessToken == "" {
		t.Fatal("the caller's session must keep its access token")
	}
}
