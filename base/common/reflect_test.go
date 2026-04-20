package common

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestGetStructName(t *testing.T) {
	type MyStruct struct{}
	type AnotherStruct struct{}

	tests := []struct {
		input    interface{}
		expected string
		err      error
	}{{MyStruct{}, "my_struct", nil},
		{AnotherStruct{}, "another_struct", nil},
		{123, "", fmt.Errorf("expected a struct, got int")},
	}

	for _, test := range tests {
		result, err := ParseStructName(test.input)

		if (err != nil) != (test.err != nil) {
			t.Errorf("ParseStructName(%v) unexpected error: %v", test.input, err)
			continue
		}

		if err == nil && result != test.expected {
			t.Errorf("ParseStructName(%v) = %s; expected %s", test.input, result, test.expected)
		}
	}
}

func TestGetColumns(t *testing.T) {
	type MyStruct struct {
		Field1 string `column:"field_1"`
		Field2 int    `column:"field_2"`
	}

	tests := []struct {
		input    interface{}
		expected string
		err      error
	}{
		{MyStruct{}, "field_1, field_2", nil},
		{123, "", fmt.Errorf("expected a struct, got int")},
	}

	for _, test := range tests {
		result, err := ParseStructColumns(test.input)

		if (err != nil) != (test.err != nil) {
			t.Errorf("ParseStructColumns(%v) unexpected error: %v", test.input, err)
			continue
		}

		if err == nil && result != test.expected {
			t.Errorf("ParseStructColumns(%v) = %s; expected %s", test.input, result, test.expected)
		}
	}
}

func TestParseStructEnv(t *testing.T) {

	type DatabaseConfig struct {
		Driver      string        `env:"SQL_DRIVER"`
		URL         string        `env:"SQL_URL"`
		Migrate     bool          `env:"SQL_MIGRATE"`
		MaxOpen     int           `env:"SQL_MAX_OPEN"`
		MaxIdle     *int          `env:"SQL_MAX_IDLE"`
		MaxLifetime time.Duration `env:"SQL_MAX_LIFETIME"`
	}

	// Setup env vars
	os.Setenv("SQL_DRIVER", "postgres")
	os.Setenv("SQL_URL", "postgres://user:pass@localhost/db")
	os.Setenv("SQL_MIGRATE", "true")
	os.Setenv("SQL_MAX_OPEN", "20")
	os.Setenv("SQL_MAX_IDLE", "5")
	os.Setenv("SQL_MAX_LIFETIME", "3m")

	defer func() { // Cleanup
		os.Unsetenv("SQL_DRIVER")
		os.Unsetenv("SQL_URL")
		os.Unsetenv("SQL_MIGRATE")
		os.Unsetenv("SQL_MAX_OPEN")
		os.Unsetenv("SQL_MAX_IDLE")
		os.Unsetenv("SQL_MAX_LIFETIME")
	}()

	cfg := &DatabaseConfig{}
	err := ParseStructAnnotation(cfg, "env")
	if err != nil {
		t.Fatalf("ParseStructEnv returned error: %v", err)
	}

	// String
	if cfg.Driver != "postgres" {
		t.Errorf("Expected Driver = postgres, got %s", cfg.Driver)
	}

	// String
	if cfg.URL != "postgres://user:pass@localhost/db" {
		t.Errorf("Expected URL match, got %s", cfg.URL)
	}

	// Bool
	if !cfg.Migrate {
		t.Errorf("Expected Migrate = true, got %v", cfg.Migrate)
	}

	// Int
	if cfg.MaxOpen != 20 {
		t.Errorf("Expected MaxOpen = 20, got %d", cfg.MaxOpen)
	}

	// Pointer int
	if cfg.MaxIdle == nil || *cfg.MaxIdle != 5 {
		t.Errorf("Expected MaxIdle = 5, got %v", cfg.MaxIdle)
	}

	// time.Duration
	expectedDuration := 3 * time.Minute
	if cfg.MaxLifetime != expectedDuration {
		t.Errorf("Expected MaxLifetime = %v, got %v", expectedDuration, cfg.MaxLifetime)
	}
}

func TestParseStructAnnotationInt64AndFloat64(t *testing.T) {
	type NumericConfig struct {
		Score   float64 `env:"SCORE"`
		Counter int64   `env:"COUNTER"`
	}

	t.Setenv("SCORE", "9.75")
	t.Setenv("COUNTER", "1234567890")

	cfg := &NumericConfig{}
	if err := ParseStructAnnotation(cfg, "env"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Score != 9.75 {
		t.Errorf("Expected Score=9.75, got %f", cfg.Score)
	}
	if cfg.Counter != 1234567890 {
		t.Errorf("Expected Counter=1234567890, got %d", cfg.Counter)
	}
}

func TestParseStructAnnotationEmptyAnnotation(t *testing.T) {
	type ConfigWithSkip struct {
		Name    string `env:"SKIP_NAME"`
		NoTag   string // no annotation tag — must be skipped silently
		Ignored string `env:""`
	}

	t.Setenv("SKIP_NAME", "value")

	cfg := &ConfigWithSkip{}
	if err := ParseStructAnnotation(cfg, "env"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "value" {
		t.Errorf("Expected Name=\"value\", got %q", cfg.Name)
	}
	if cfg.NoTag != "" || cfg.Ignored != "" {
		t.Error("Fields without annotation tags should remain empty")
	}
}

func TestParseStructAnnotationNonPointerError(t *testing.T) {
	type Config struct {
		Name string `env:"NAME"`
	}
	err := ParseStructAnnotation(Config{}, "env")
	if err == nil {
		t.Error("Expected error when passing non-pointer struct")
	}
}

func TestParseStructAnnotationInvalidBool(t *testing.T) {
	type Config struct {
		Flag bool `env:"BAD_BOOL"`
	}
	t.Setenv("BAD_BOOL", "not-a-bool")
	cfg := &Config{}
	if err := ParseStructAnnotation(cfg, "env"); err == nil {
		t.Error("Expected error for invalid bool value")
	}
}

func TestParseStructAnnotationInvalidInt(t *testing.T) {
	type Config struct {
		Count int `env:"BAD_INT"`
	}
	t.Setenv("BAD_INT", "abc")
	cfg := &Config{}
	if err := ParseStructAnnotation(cfg, "env"); err == nil {
		t.Error("Expected error for invalid int value")
	}
}

func TestParseStructAnnotationInvalidInt64(t *testing.T) {
	type Config struct {
		Count int64 `env:"BAD_INT64"`
	}
	t.Setenv("BAD_INT64", "not-a-number")
	cfg := &Config{}
	if err := ParseStructAnnotation(cfg, "env"); err == nil {
		t.Error("Expected error for invalid int64 value")
	}
}

func TestParseStructAnnotationInvalidFloat64(t *testing.T) {
	type Config struct {
		Score float64 `env:"BAD_FLOAT"`
	}
	t.Setenv("BAD_FLOAT", "xyz")
	cfg := &Config{}
	if err := ParseStructAnnotation(cfg, "env"); err == nil {
		t.Error("Expected error for invalid float64 value")
	}
}

func TestParseStructAnnotationInvalidDuration(t *testing.T) {
	type Config struct {
		Timeout time.Duration `env:"BAD_DURATION"`
	}
	t.Setenv("BAD_DURATION", "notaduration")
	cfg := &Config{}
	if err := ParseStructAnnotation(cfg, "env"); err == nil {
		t.Error("Expected error for invalid duration value")
	}
}

func TestParseStructAnnotationUnsupportedType(t *testing.T) {
	type Config struct {
		Ch chan int `env:"UNSUPPORTED_CHAN"`
	}
	t.Setenv("UNSUPPORTED_CHAN", "somevalue")
	cfg := &Config{}
	if err := ParseStructAnnotation(cfg, "env"); err == nil {
		t.Error("Expected error for unsupported field type")
	}
}
