package common

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseStructAnnotationErrorsNeverEchoTheValue(t *testing.T) {
	type Config struct {
		B bool          `env:"LEAK_BOOL"`
		I int           `env:"LEAK_INT"`
		L int64         `env:"LEAK_INT64"`
		F float64       `env:"LEAK_FLOAT"`
		D time.Duration `env:"LEAK_DURATION"`
	}
	const secret = "hunter2-s3cr3t"
	vars := map[string]string{
		"LEAK_BOOL": "bool", "LEAK_INT": "int", "LEAK_INT64": "int64",
		"LEAK_FLOAT": "float64", "LEAK_DURATION": "duration",
	}
	lookup := func(string) string { return secret }
	err := ParseStructAnnotationFunc(&Config{}, "env", lookup)
	if err == nil {
		t.Fatal("expected parse errors")
	}
	msg := err.Error()
	if strings.Contains(msg, "hunter2") {
		t.Fatalf("error echoes the raw value: %s", msg)
	}
	for name, typ := range vars {
		if !strings.Contains(msg, "error parsing "+typ+" for "+name) {
			t.Errorf("missing variable/type %s/%s in %s", name, typ, msg)
		}
	}
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Errorf("numeric errors should keep strconv.ErrSyntax: %v", err)
	}
}
