// Package config is gofi's composition layer between the environment loader
// (base/environment) and each library's typed Config: the libraries (mail,
// bucket, iam, ...) stay decoupled from environment, while this package maps
// env vars into their explicit Config structs.
//
// The mappings of the resources started by gofi live with their components
// (component/database.ConfigFromEnv, component/messaging.ConfigFromEnv, ...),
// and the process-wide settings in config/core, so that importing gofi links
// only what the service uses.
//
// Each adapter takes an *environment.Environment explicitly so it can be tested
// without the process-wide singleton. Applications that don't use gofi's
// environment loader can ignore this package and build each library's Config
// themselves.
package config
