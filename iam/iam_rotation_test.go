package iam

import (
	"strings"
	"testing"
)

func TestPreviousKeys(t *testing.T) {
	if previousKeys(DefaultConfig{}) != nil {
		t.Error("no previous secret, no verification keys")
	}
	keys := previousKeys(DefaultConfig{JWTPreviousKeyID: "k1", JWTPreviousSecret: strings.Repeat("a", 32)})
	if len(keys) != 1 || keys["k1"] == nil {
		t.Errorf("keys=%v", keys)
	}
}
