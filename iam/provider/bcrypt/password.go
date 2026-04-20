// Package bcrypt provides password hashing utilities for use in UserPort implementations.
// Developers use this package in their implementation of port.UserPort.ValidatePassword.
package bcrypt

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultCost is the default bcrypt cost (12). Adjust based on available hardware.
	// Costs below 12 are rejected in production — use MinCost only in tests.
	DefaultCost = 12
	MinCost     = bcrypt.MinCost
)

// Hash generates the bcrypt hash of a password with the given cost.
// Returns an error if the cost is invalid or salt generation fails.
func Hash(password string, cost int) (string, error) {
	if cost < MinCost {
		return "", fmt.Errorf("iam/bcrypt: cost %d is below minimum (%d)", cost, MinCost)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("iam/bcrypt: failed to hash password: %w", err)
	}
	return string(h), nil
}

// HashDefault generates the hash using DefaultCost (12).
func HashDefault(password string) (string, error) {
	return Hash(password, DefaultCost)
}

// Compare checks whether the password matches the hash using timing-safe comparison.
// Returns nil if the password is valid, or an error otherwise.
// Never returns details that would allow distinguishing an invalid password from a corrupted hash.
func Compare(hash, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return fmt.Errorf("iam/bcrypt: invalid credentials")
	}
	return nil
}
