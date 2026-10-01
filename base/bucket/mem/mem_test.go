package mem_test

import (
	"context"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/base/bucket"
	"github.com/gofi-labs/gofi-sdk-go/base/bucket/buckettest"
	"github.com/gofi-labs/gofi-sdk-go/base/bucket/mem"
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
