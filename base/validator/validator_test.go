package validator

import (
	"fmt"
	"strings"
	"testing"
)

type AllCases struct {
	RequiredString string            `validate:"required"`               // Must be present and not empty
	MinLength      string            `validate:"min=3"`                  // Must be at least 3 characters long
	MaxLength      string            `validate:"max=5"`                  // Must be at most 5 characters long
	FixedLength    string            `validate:"len=4"`                  // Must be exactly 4 characters long
	Email          string            `validate:"email"`                  // Must be a valid email format
	URL            string            `validate:"url"`                    // Must be a valid URL
	IntGreaterThan int               `validate:"gt=10"`                  // Must be greater than 10
	IntLessOrEqual int               `validate:"lte=100"`                // Must be less than or equal to 100
	FloatGTE       float64           `validate:"gte=1.5"`                // Must be greater than or equal to 1.5
	StringInList   string            `validate:"oneof=admin user guest"` // Must be one of the listed values: "admin", "user", or "guest"
	UUIDValue      string            `validate:"uuid4"`                  // Must be a valid UUID version 4
	BoolValue      bool              `validate:"boolean"`                // Must be a valid boolean (true or false)
	SliceMinItems  []int             `validate:"min=2,dive,gt=0"`        // Slice must have at least 2 elements, each greater than 0
	MapLen         map[string]string `validate:"len=2"`                  // Map must contain exactly 2 key-value pairs
	OptionalField  string            `validate:"omitempty,email"`        // Optional; if present, must be a valid email
}

func TestValidateAllCases(t *testing.T) {
	v := New()

	cases := AllCases{
		RequiredString: "",
		MinLength:      "ab",
		MaxLength:      "abcdef",
		FixedLength:    "abc",
		Email:          "invalid_email",
		URL:            "invalid-url",
		IntGreaterThan: 5,
		IntLessOrEqual: 101,
		FloatGTE:       1.0,
		StringInList:   "manager",
		UUIDValue:      "1234",
		BoolValue:      true,
		SliceMinItems:  []int{0},
		MapLen:         map[string]string{"a": "1"},
		OptionalField:  "invalidemail",
	}

	err := v.ValidateStruct(cases)
	if err == nil {
		t.Error("I expected a validation error, but the struct passed")
		return
	}

	valErrs, ok := err.(ValidationError)
	if !ok {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	fmt.Println("Validation errors:")
	for _, fe := range valErrs.Errors {
		fmt.Printf(" - Field: %s | Error: %s\n", fe.Field, fe.Message)
	}
}

func TestValidateStructValid(t *testing.T) {
	type SimpleStruct struct {
		Name  string `validate:"required"`
		Email string `validate:"required,email"`
	}

	v := New()
	s := SimpleStruct{Name: "Emilia", Email: "Emilia@example.com"}
	if err := v.ValidateStruct(s); err != nil {
		t.Errorf("Expected no validation error for valid struct, got: %v", err)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	ve := ValidationError{Errors: []FieldError{{Field: "Name", Message: "required"}}}
	if ve.Error() != "validation failed" {
		t.Errorf("Expected \"validation failed\", got %q", ve.Error())
	}
}

func TestIsStruct(t *testing.T) {
	type S struct{}

	if err := IsStruct(S{}); err != nil {
		t.Errorf("IsStruct(struct) unexpected error: %v", err)
	}

	if err := IsStruct(42); err == nil {
		t.Error("Expected error for non-struct input")
	}

	if err := IsStruct("string"); err == nil {
		t.Error("Expected error for string input")
	}
}

func TestIsStructP(t *testing.T) {
	type S struct{}

	if err := IsStructP(&S{}); err != nil {
		t.Errorf("IsStructP(*struct) unexpected error: %v", err)
	}

	if err := IsStructP(S{}); err == nil {
		t.Error("Expected error for non-pointer struct")
	}

	if err := IsStructP(42); err == nil {
		t.Error("Expected error for int input")
	}

	if err := IsStructP(new(int)); err == nil {
		t.Error("Expected error for pointer to non-struct")
	}
}

func TestFieldErrorFields(t *testing.T) {
	fe := FieldError{Field: "Username", Message: "required"}
	if fe.Field != "Username" {
		t.Errorf("Expected Field=\"Username\", got %q", fe.Field)
	}
	if fe.Message != "required" {
		t.Errorf("Expected Message=\"required\", got %q", fe.Message)
	}
}

func TestFormatValidationErrorsMessage(t *testing.T) {
	v := New()

	type S struct {
		Name string `validate:"required"`
	}

	err := v.ValidateStruct(S{})
	if err == nil {
		t.Fatal("Expected validation error")
	}

	ve, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("Expected ValidationError, got %T", err)
	}

	if len(ve.Errors) == 0 {
		t.Fatal("Expected at least one FieldError")
	}

	msg := ve.Errors[0].Message
	if !strings.Contains(msg, "Name") {
		t.Errorf("Expected message to contain field name, got: %q", msg)
	}
}
