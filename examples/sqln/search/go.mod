module github.com/gofi-labs/gofi-sdk-go/examples/sqln/search

go 1.26.6

require (
	github.com/gofi-labs/gofi-sdk-go/gofi v0.8.2
	github.com/gofi-labs/gofi-sdk-go/sqln v0.8.2
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/base v0.8.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/obs v0.8.2 // indirect
	github.com/golang-migrate/migrate/v4 v4.19.1 // indirect
	github.com/jackc/pgerrcode v0.0.0-20220416144525-469b46aa5efa // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.11.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/redis/go-redis/v9 v9.18.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../../../base
	github.com/gofi-labs/gofi-sdk-go/gofi => ../../../gofi
	github.com/gofi-labs/gofi-sdk-go/iam => ../../../iam
	github.com/gofi-labs/gofi-sdk-go/msq => ../../../msq
	github.com/gofi-labs/gofi-sdk-go/netx => ../../../netx
	github.com/gofi-labs/gofi-sdk-go/obs => ../../../obs
	github.com/gofi-labs/gofi-sdk-go/sqln => ../../../sqln
)
