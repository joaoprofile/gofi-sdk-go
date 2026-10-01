module github.com/gofi-labs/gofi-sdk-go/msq/provider/oci

go 1.26.6

require (
	github.com/gofi-labs/gofi-sdk-go/base/cloud/oci v0.2.1
	github.com/gofi-labs/gofi-sdk-go/msq v0.2.1
	github.com/gofi-labs/gofi-sdk-go/obs v0.2.1
	github.com/google/uuid v1.6.0
	github.com/oracle/oci-go-sdk/v65 v65.112.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/base v0.2.1 // indirect
	github.com/gofrs/flock v0.10.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/sony/gobreaker v0.5.0 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../../../base
	github.com/gofi-labs/gofi-sdk-go/base/cloud/oci => ../../../base/cloud/oci
	github.com/gofi-labs/gofi-sdk-go/msq => ../..
	github.com/gofi-labs/gofi-sdk-go/obs => ../../../obs
)
