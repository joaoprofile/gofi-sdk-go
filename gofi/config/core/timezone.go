package core

import (
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/base/timezone"
)

// Timezone builds a timezone.Config from the TIMEZONE variable of the given
// environment. An empty value means UTC.
func Timezone(env *environment.Environment) timezone.Config {
	return timezone.Config{Name: env.Timezone}
}

// SetTimezone applies the process-wide local timezone (time.Local) from the
// environment. This is the env-driven entry point gofi's builder uses.
func SetTimezone(env *environment.Environment) error {
	return timezone.Apply(Timezone(env))
}
