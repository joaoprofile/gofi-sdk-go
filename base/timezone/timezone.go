package timezone

import (
	"fmt"
	"time"
)

// BrazilName is the IANA name for Brazil's primary timezone and the default
// applied when no name is configured.
const BrazilName = "America/Sao_Paulo"

// Config configures the process-wide local timezone.
type Config struct {
	// Name is the IANA timezone name (e.g. "America/Sao_Paulo"). When empty,
	// BrazilName is used.
	Name string
}

// Apply sets time.Local from the configured timezone.
//
// When Name is empty (or BrazilName) it defaults to Brazil; if Brazil's tzdata
// cannot be loaded it falls back to a fixed UTC-3 zone and returns nil. A
// non-empty but invalid Name returns an error and leaves time.Local untouched.
func Apply(cfg Config) error {
	name := cfg.Name
	if name == "" || name == BrazilName {
		loc, err := time.LoadLocation(BrazilName)
		if err != nil {
			time.Local = time.FixedZone("UTC-3", -3*60*60)
			return nil
		}
		time.Local = loc
		return nil
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	time.Local = loc
	return nil
}
