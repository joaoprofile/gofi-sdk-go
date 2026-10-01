package timezone

import (
	"strings"
	"testing"
	"time"
)

func TestApplyDefaultIsUTC(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	if err := Apply(Config{}); err != nil {
		t.Fatalf("Apply(Config{}) returned unexpected error: %v", err)
	}
	if time.Local != time.UTC {
		t.Errorf("default timezone must be UTC, got %s", time.Local)
	}
}

func TestApplyExplicitBrazilKeepsLegacyZone(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	if err := Apply(Config{Name: BrazilName}); err != nil {
		t.Fatal(err)
	}
	if time.Local.String() != BrazilName {
		t.Errorf("time.Local=%s, want %s (embedded tzdata)", time.Local, BrazilName)
	}
}

func TestApplyValidName(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	if err := Apply(Config{Name: "UTC"}); err != nil {
		t.Fatalf("Apply(UTC) returned unexpected error: %v", err)
	}
	if time.Local == nil {
		t.Error("Expected time.Local to be set after Apply(UTC)")
	}
}

func TestApplyInvalidName(t *testing.T) {
	err := Apply(Config{Name: "Invalid/Timezone/XYZ"})
	if err == nil {
		t.Error("Expected error for invalid timezone name, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "invalid timezone") {
		t.Errorf("Expected error message to contain \"invalid timezone\", got: %v", err)
	}
}
