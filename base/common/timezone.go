package common

import (
	"fmt"
	"time"
)

var (
	BrazilTimeZoneName = "America/Sao_Paulo"
)

func SetBrazil() error {
	loc, err := time.LoadLocation(BrazilTimeZoneName)
	if err != nil {
		time.Local = time.FixedZone("UTC-3", -3*60*60)
		return nil
	}

	time.Local = loc
	return nil
}

func SetWithName(name string) error {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	time.Local = loc
	return nil
}
