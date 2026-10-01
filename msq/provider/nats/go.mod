module github.com/gofi-labs/gofi-sdk-go/msq/provider/nats

go 1.26.6

require (
	github.com/gofi-labs/gofi-sdk-go/msq v0.8.2
	github.com/gofi-labs/gofi-sdk-go/obs v0.8.2
	github.com/google/uuid v1.6.0
	github.com/nats-io/nats-server/v2 v2.15.0
	github.com/nats-io/nats.go v1.54.0
)

require (
	github.com/antithesishq/antithesis-sdk-go v0.8.0-default-no-op // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/base v0.8.2 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/klauspost/compress v1.20.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../../../base
	github.com/gofi-labs/gofi-sdk-go/msq => ../..
	github.com/gofi-labs/gofi-sdk-go/obs => ../../../obs
)
