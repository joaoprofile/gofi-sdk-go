package config

import (
	"github.com/joaoprofile/gofi/base/cloud"
	"github.com/joaoprofile/gofi/base/environment"
)

// Cloud builds a cloud.Config from the CLOUD_* variables of the environment.
func Cloud(env *environment.Environment) cloud.Config {
	return cloud.Config{
		Provider:   cloud.ProviderName(env.CloudProvider),
		Host:       env.CloudHost,
		Region:     env.CloudRegion,
		Secret:     env.CloudSecret,
		Token:      env.CloudToken,
		DisableSSL: env.CloudDisableSSL,
	}
}

// InitCloud initialises the cloud singleton from the environment. The result is
// sticky (see cloud.Init); a no-provider configuration returns an error that
// callers may treat as "cloud disabled".
func InitCloud(env *environment.Environment) (*cloud.Cloud, error) {
	return cloud.Init(Cloud(env))
}
