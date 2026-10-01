// Package core holds the process-wide settings gofi's Build applies before any
// component starts: timezone, logging and TLS verification. It depends only on
// base/environment, base/timezone and obs/logging, so importing it links no
// resource; package config re-exports these functions.
package core
