// Package config is gofi's composition layer between the environment loader
// (base/environment) and each library's typed Config. It is the single place
// that knows about both: the libraries (mail, bucket, cloud, ...) stay fully
// decoupled from environment, while this package maps env vars into their
// explicit Config structs.
//
// Each adapter takes an *environment.Environment explicitly so it can be tested
// without the process-wide singleton; gofi's builder passes
// environment.Instance(). Applications that don't use gofi's environment loader
// can ignore this package and build each library's Config themselves.
package config
