package netx

import (
	"encoding/json"
	"errors"
	"fmt"
)

type HttpError struct {
	Message string   `json:"message"`
	Err     string   `json:"error"`
	Status  int      `json:"status"`
	Cause   []string `json:"cause"`
}

func NewAPIError(status int, message string, err error) *HttpError {
	if message == "" {
		message = "Internal Server Error"
	}
	if status == 0 {
		status = 500
	}
	if err == nil {
		err = errors.New("Error not cataloged")
	}

	return &HttpError{
		Message: message,
		Status:  status,
		Err:     err.Error(),
	}
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("%s (status: %d, error: %s)", e.Message, e.Status, e.Err)
}

func ParseError(jsonStr string) (*HttpError, error) {
	var apiError HttpError

	err := json.Unmarshal([]byte(jsonStr), &apiError)
	if err == nil {
		return &apiError, nil
	}

	var unescapedStr string
	if err := json.Unmarshal([]byte(jsonStr), &unescapedStr); err == nil {
		if err := json.Unmarshal([]byte(unescapedStr), &apiError); err == nil {
			return &apiError, nil
		}
	}

	return nil, fmt.Errorf("failed to parse error JSON: %w", err)
}

func FromError(err error) *HttpError {
	if err == nil {
		return nil
	}

	if httpErr, ok := err.(*HttpError); ok {
		return httpErr
	}

	parsedErr, parseErr := ParseError(err.Error())
	if parseErr == nil {
		return parsedErr
	}

	return &HttpError{
		Message: err.Error(),
		Status:  500,
		Err:     "Internal Server Error",
		Cause:   []string{"error not cataloged"},
	}
}
