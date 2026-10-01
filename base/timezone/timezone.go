package timezone

import (
	"fmt"
	"time"
	_ "time/tzdata" // IANA database for images without /usr/share/zoneinfo
)

// BrazilName is the IANA name for Brazil's primary timezone and the default
// applied when no name is configured.
const BrazilName = "America/Sao_Paulo"

// Config configures the process-wide local timezone.
type Config struct {
	// Name is the IANA timezone name (e.g. "America/Sao_Paulo"). When empty,
	// UTC is used.
	Name string
}

// Apply sets time.Local from the configured timezone. An empty Name selects
// UTC; an invalid Name returns an error and leaves time.Local untouched. The
// IANA database is embedded, so names resolve in scratch/distroless images.
func Apply(cfg Config) error {
	loc := time.UTC
	if cfg.Name != "" && cfg.Name != "UTC" {
		var err error
		if loc, err = time.LoadLocation(cfg.Name); err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}
	}
	// time.Local is read by every time.Now: only write it when it changes, so a
	// repeated Apply does not race with running goroutines.
	if time.Local.String() != loc.String() {
		time.Local = loc
	}
	return nil
}
