package validator

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

type Validator struct {
	validate *validator.Validate
}

func New() *Validator {
	return &Validator{
		validate: validator.New(),
	}
}

func (v *Validator) ValidateStruct(s interface{}) error {
	err := v.validate.Struct(s)
	if err == nil {
		return nil
	}

	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return fmt.Errorf("validation failed: %w", err)
	}

	return formatValidationErrors(validationErrors)
}

func formatValidationErrors(errs validator.ValidationErrors) error {
	var fieldErrors []FieldError

	for _, e := range errs {
		fieldErrors = append(fieldErrors, FieldError{
			Field:   e.Field(),
			Message: fmt.Sprintf("Field '%s' failed on the '%s' rule", e.Field(), e.Tag()),
		})
	}
	if len(fieldErrors) > 0 {
		return ValidationError{Errors: fieldErrors}
	}

	return nil
}
