package bucket

import (
	"errors"
	"testing"
	"time"
)

func TestPresignLimit(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:                         MaxPresignTTL,
		-time.Hour:                MaxPresignTTL,
		time.Hour:                 time.Hour,
		MaxPresignTTL:             MaxPresignTTL,
		MaxPresignTTL + time.Hour: MaxPresignTTL,
	}
	for limit, want := range cases {
		if got := PresignLimit(limit); got != want {
			t.Errorf("PresignLimit(%s)=%s; want %s", limit, got, want)
		}
	}
}

func TestCheckPresignTTL(t *testing.T) {
	cases := []struct {
		ttl, limit time.Duration
		ok         bool
	}{
		{time.Minute, 0, true},
		{MaxPresignTTL, 0, true},
		{MaxPresignTTL + time.Second, 0, false},
		{0, 0, false},
		{-time.Second, 0, false},
		{time.Hour, time.Hour, true},
		{time.Hour + time.Second, time.Hour, false},
		// A limit above the hard cap cannot widen it.
		{MaxPresignTTL + time.Second, 30 * 24 * time.Hour, false},
	}
	for _, c := range cases {
		err := CheckPresignTTL(c.ttl, c.limit)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidTTL)) {
			t.Errorf("CheckPresignTTL(%s, %s)=%v; want ok=%v", c.ttl, c.limit, err, c.ok)
		}
	}
}
