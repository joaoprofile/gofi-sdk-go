package common

import (
	"encoding/json"
	"testing"
)

func TestStringScanNil(t *testing.T) {
	var s String
	if err := s.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) unexpected error: %v", err)
	}
	if s.Valid {
		t.Error("Expected Valid=false after Scan(nil)")
	}
}

func TestStringScanValue(t *testing.T) {
	var s String
	if err := s.Scan("hello"); err != nil {
		t.Fatalf("Scan(\"hello\") unexpected error: %v", err)
	}
	if !s.Valid {
		t.Error("Expected Valid=true after Scan(string value)")
	}
	if s.String != "hello" {
		t.Errorf("Expected String=\"hello\", got %q", s.String)
	}
}

func TestStringValueValid(t *testing.T) {
	s := String{String: "world", Valid: true}
	val, err := s.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error: %v", err)
	}
	if val != "world" {
		t.Errorf("Expected Value=\"world\", got %v", val)
	}
}

func TestStringValueInvalid(t *testing.T) {
	s := String{String: "ignored", Valid: false}
	val, err := s.Value()
	if err != nil {
		t.Fatalf("Value() unexpected error for invalid String: %v", err)
	}
	if val != nil {
		t.Errorf("Expected nil Value for invalid String, got %v", val)
	}
}

func TestStringMarshalJSONValid(t *testing.T) {
	s := String{String: "test", Valid: true}
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() unexpected error: %v", err)
	}

	var result string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if result != "test" {
		t.Errorf("Expected \"test\", got %q", result)
	}
}

func TestStringMarshalJSONInvalid(t *testing.T) {
	s := String{Valid: false}
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() unexpected error for invalid String: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("Expected \"null\", got %s", string(data))
	}
}

func TestStringUnmarshalJSONValue(t *testing.T) {
	var s String
	if err := s.UnmarshalJSON([]byte(`"hello"`)); err != nil {
		t.Fatalf("UnmarshalJSON(string) unexpected error: %v", err)
	}
	if !s.Valid {
		t.Error("Expected Valid=true after UnmarshalJSON with string")
	}
	if s.String != "hello" {
		t.Errorf("Expected String=\"hello\", got %q", s.String)
	}
}

func TestStringUnmarshalJSONNull(t *testing.T) {
	var s String
	if err := s.UnmarshalJSON([]byte(`null`)); err != nil {
		t.Fatalf("UnmarshalJSON(null) unexpected error: %v", err)
	}
	if s.Valid {
		t.Error("Expected Valid=false after UnmarshalJSON with null")
	}
}

func TestStringUnmarshalJSONInvalid(t *testing.T) {
	var s String
	if err := s.UnmarshalJSON([]byte(`{invalid}`)); err == nil {
		t.Error("Expected error for invalid JSON input")
	}
}
