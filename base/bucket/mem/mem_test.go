package mem_test

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/bucket"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket/buckettest"
	"github.com/joaoprofile/gofi-sdk-go/base/bucket/mem"
)

func TestContract(t *testing.T) { buckettest.Run(t, mem.New("test")) }

func TestOpenURL(t *testing.T) {
	s, err := bucket.OpenURL(context.Background(), "mem://test")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*mem.Store); !ok {
		t.Fatalf("got %T", s)
	}
}

func TestManagerContract(t *testing.T) { buckettest.RunManager(t, mem.NewManager()) }

func TestOpenManager(t *testing.T) {
	m, err := bucket.OpenManager(context.Background(), bucket.Config{Provider: bucket.ProviderMem})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*mem.Manager); !ok {
		t.Fatalf("got %T", m)
	}
}
