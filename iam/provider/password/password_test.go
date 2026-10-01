package password

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

var fast = Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashAndVerify(t *testing.T) {
	h, err := fast.Hash("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("hash=%q", h)
	}
	if err := Verify(h, "s3cret"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}
	if err := Verify(h, "wrong"); err != ErrInvalid {
		t.Errorf("wrong password: %v", err)
	}
	if h2, _ := fast.Hash("s3cret"); h2 == h {
		t.Error("salt must differ per hash")
	}
}

func TestVerifyBcryptAndMigrate(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err := Verify(string(legacy), "s3cret"); err != nil {
		t.Fatalf("bcrypt hash must verify: %v", err)
	}
	if Verify(string(legacy), "wrong") != ErrInvalid {
		t.Error("wrong password on bcrypt hash")
	}
	if !NeedsRehash(string(legacy)) {
		t.Error("bcrypt hashes need rehash")
	}
}

func TestNeedsRehash(t *testing.T) {
	weak, _ := fast.Hash("x")
	if !DefaultParams.NeedsRehash(weak) {
		t.Error("weaker params need rehash")
	}
	if fast.NeedsRehash(weak) {
		t.Error("same params do not need rehash")
	}
	if !NeedsRehash("garbage") {
		t.Error("unreadable hash needs rehash")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, h := range []string{"", "$argon2id$v=19$m=0,t=1,p=1$c2FsdA$a2V5", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=18$m=64,t=1,p=1$c2FsdA$a2V5"} {
		if Verify(h, "x") != ErrInvalid {
			t.Errorf("Verify(%q) must fail", h)
		}
	}
}

func TestVerifyRejectsUnboundedParams(t *testing.T) {
	for _, h := range []string{
		"$argon2id$v=19$m=4294967295,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=64,t=4294967295,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$" + strings.Repeat("A", 2000) + "$a2V5",
		"$argon2id$v=19$m=64,t=1,p=1$c2FsdA$" + strings.Repeat("A", 2000),
	} {
		if Verify(h, "x") != ErrInvalid {
			t.Errorf("Verify must reject out-of-bounds hash %.60q", h)
		}
	}
}

func TestDefaultParamsRoundTrip(t *testing.T) {
	h, err := Hash("pw")
	if err != nil || Verify(h, "pw") != nil || NeedsRehash(h) {
		t.Fatalf("default hash round trip failed: %q %v", h, err)
	}
}
