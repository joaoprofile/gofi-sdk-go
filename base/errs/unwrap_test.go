package errs

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
)

var errOrderNotFound = RegisterNotFound("order-not-found-test", "order %s not found")

func TestAppError_IsMatchesByCode(t *testing.T) {
	e := errOrderNotFound.New("42")
	var err error = fmt.Errorf("service: %w", &e)

	if !errors.Is(err, &errOrderNotFound) {
		t.Fatal("errors.Is must match a registered error by code")
	}
	other := RegisterNotFound("other-code-test", "x")
	if errors.Is(err, &other) {
		t.Fatal("different codes must not match")
	}
}

func TestAppError_UnwrapReachesCause(t *testing.T) {
	e := errOrderNotFound.Wrap(sql.ErrNoRows, "42")
	var err error = &e
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("errors.Is must reach the wrapped cause")
	}
}

func TestJoinErrors(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	err := JoinErrors([]error{a, nil, b})
	if err.Error() != "a | b" {
		t.Fatalf("message=%q", err.Error())
	}
	if !errors.Is(err, a) || !errors.Is(err, b) {
		t.Fatal("joined errors must stay matchable")
	}
}

func TestRegister_ConcurrentSafe(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			Register(fmt.Sprintf("concurrent-%d", i), "m")
			_ = GetErrorByCode(fmt.Sprintf("concurrent-%d", i))
		})
	}
	wg.Wait()
}

func TestAppError_ValueIsAnError(t *testing.T) {
	var err error = errOrderNotFound.New("7") // no & needed
	if !errors.Is(err, errOrderNotFound) {
		t.Fatal("errors.Is must accept the registered value as target")
	}
	var got AppError
	if !errors.As(fmt.Errorf("wrap: %w", err), &got) || got.Code != errOrderNotFound.Code {
		t.Fatal("errors.As must extract an AppError value")
	}
}
