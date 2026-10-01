module github.com/joaoprofile/gofi-sdk-go/netx

go 1.26.6

require (
	github.com/alicebob/miniredis/v2 v2.37.0
	github.com/go-chi/chi/v5 v5.3.2
	github.com/go-redis/redis_rate/v10 v10.0.1
	github.com/joaoprofile/gofi-sdk-go/base v0.2.3
	github.com/joaoprofile/gofi-sdk-go/obs v0.2.3
	github.com/redis/go-redis/v9 v9.18.0
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0
	go.opentelemetry.io/otel v1.46.0
	go.opentelemetry.io/otel/trace v1.46.0
	golang.org/x/time v0.16.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/joaoprofile/gofi-sdk-go/base => ../base
	github.com/joaoprofile/gofi-sdk-go/obs => ../obs
)
