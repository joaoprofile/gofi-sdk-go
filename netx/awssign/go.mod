module github.com/gofi-labs/gofi-sdk-go/netx/awssign

go 1.26.6

require (
	github.com/aws/aws-sdk-go-v2 v1.47.1
	github.com/aws/aws-sdk-go-v2/credentials v1.20.6
	github.com/gofi-labs/gofi-sdk-go/base/cloud/aws v0.8.2
	github.com/gofi-labs/gofi-sdk-go/netx v0.8.2
)

require (
	github.com/aws/aws-sdk-go-v2/config v1.33.6 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.1 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-chi/chi/v5 v5.3.2 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/go-redis/redis_rate/v10 v10.0.1 // indirect
	github.com/gofi-labs/gofi-sdk-go/base v0.8.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/obs v0.8.2 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/redis/go-redis/v9 v9.18.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../../base
	github.com/gofi-labs/gofi-sdk-go/base/cloud/aws => ../../base/cloud/aws
	github.com/gofi-labs/gofi-sdk-go/netx => ..
	github.com/gofi-labs/gofi-sdk-go/obs => ../../obs
)
