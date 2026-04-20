package common

import (
	"strings"
	"testing"
	"time"
)

func TestSetBrazil(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	if err := SetBrazil(); err != nil {
		t.Fatalf("SetBrazil() returned unexpected error: %v", err)
	}
	if time.Local == nil {
		t.Error("Expected time.Local to be set after SetBrazil()")
	}
}

func TestSetWithNameValid(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	if err := SetWithName("UTC"); err != nil {
		t.Fatalf("SetWithName(\"UTC\") returned unexpected error: %v", err)
	}
	if time.Local == nil {
		t.Error("Expected time.Local to be set after SetWithName(\"UTC\")")
	}
}

func TestSetWithNameInvalid(t *testing.T) {
	err := SetWithName("Invalid/Timezone/XYZ")
	if err == nil {
		t.Error("Expected error for invalid timezone name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid timezone") {
		t.Errorf("Expected error message to contain \"invalid timezone\", got: %v", err)
	}
}
