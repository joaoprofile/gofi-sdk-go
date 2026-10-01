module github.com/joaoprofile/gofi-sdk-go/examples/msq/rabbitmq/producer

go 1.26.6

require (
	github.com/joaoprofile/gofi-sdk-go/gofi v0.2.3
	github.com/joaoprofile/gofi-sdk-go/msq v0.2.3
	github.com/joaoprofile/gofi-sdk-go/msq/provider/rabbitmq v0.2.3
	github.com/joaoprofile/gofi-sdk-go/obs v0.2.3
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/joaoprofile/gofi-sdk-go/base v0.2.3 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/rabbitmq/amqp091-go v1.13.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/joaoprofile/gofi-sdk-go/base => ../../../../base
	github.com/joaoprofile/gofi-sdk-go/gofi => ../../../../gofi
	github.com/joaoprofile/gofi-sdk-go/iam => ../../../../iam
	github.com/joaoprofile/gofi-sdk-go/msq => ../../../../msq
	github.com/joaoprofile/gofi-sdk-go/msq/provider/rabbitmq => ../../../../msq/provider/rabbitmq
	github.com/joaoprofile/gofi-sdk-go/netx => ../../../../netx
	github.com/joaoprofile/gofi-sdk-go/obs => ../../../../obs
	github.com/joaoprofile/gofi-sdk-go/sqln => ../../../../sqln
)
