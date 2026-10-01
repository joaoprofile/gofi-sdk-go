package bucket

import (
	"errors"
	"testing"
)

func TestBucketErrors(t *testing.T) {
	if !errors.Is(ErrBucketNotFound, ErrNotFound) {
		t.Error("ErrBucketNotFound must wrap ErrNotFound")
	}
	for _, err := range []error{ErrBucketExists, ErrBucketNotEmpty, ErrInvalidBucketName, ErrAccessDenied, ErrNotSupported} {
		if errors.Is(err, ErrNotFound) {
			t.Errorf("%v must not be ErrNotFound", err)
		}
	}
}
