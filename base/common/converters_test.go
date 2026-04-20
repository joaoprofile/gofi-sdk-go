package common

import "testing"

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"CamelCase", "camel_case"},
		{"camelCase", "camel_case"},
		{"Camel", "camel"},
		{"", ""},
	}

	for _, test := range tests {
		result := CamelToSnake(test.input)
		if result != test.expected {
			t.Errorf("camelToSnake(%s) = %s; expected %s", test.input, result, test.expected)
		}
	}
}

func TestToFixed(t *testing.T) {
	tests := []struct {
		num       float64
		precision int
		expected  float64
	}{
		{3.14159, 2, 3.14},
		{1.999, 1, 1.9},
		{1.0, 0, 1.0},
		{0.0, 2, 0.0},
		{2.5555, 3, 2.555},
	}

	for _, test := range tests {
		result := ToFixed(test.num, test.precision)
		if result != test.expected {
			t.Errorf("ToFixed(%f, %d) = %f; expected %f", test.num, test.precision, result, test.expected)
		}
	}
}

func TestPtr(t *testing.T) {
	s := "hello"
	ptr := Ptr(s)
	if ptr == nil {
		t.Fatal("Ptr returned nil")
	}
	if *ptr != s {
		t.Errorf("Expected *ptr = %q, got %q", s, *ptr)
	}

	empty := Ptr("")
	if empty == nil || *empty != "" {
		t.Errorf("Expected Ptr(\"\") to return pointer to empty string")
	}
}
