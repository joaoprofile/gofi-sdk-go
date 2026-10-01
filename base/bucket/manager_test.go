package bucket

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"abc", "my-bucket.v2", "0a1", strings.Repeat("a", 63)} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q)=%v", ok, err)
		}
	}
	for _, bad := range []string{"", "ab", strings.Repeat("a", 64), "A_b", "Abc", "a_b", "-ab", "ab-", ".ab", "a..b", "192.168.5.4", "a/b", "../x"} {
		if err := ValidateName(bad); !errors.Is(err, ErrInvalidBucketName) {
			t.Errorf("ValidateName(%q)=%v; want ErrInvalidBucketName", bad, err)
		}
	}
}
