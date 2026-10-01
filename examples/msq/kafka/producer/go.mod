module github.com/gofi-labs/gofi-sdk-go/examples/msq/kafka/producer

go 1.26.6

require (
	github.com/gofi-labs/gofi-sdk-go/gofi v0.2.1
	github.com/gofi-labs/gofi-sdk-go/msq v0.2.1
	github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka v0.2.1
	github.com/gofi-labs/gofi-sdk-go/obs v0.2.1
)

require (
	github.com/IBM/sarama v1.47.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/base v0.2.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/klauspost/compress v1.20.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../../../../base
	github.com/gofi-labs/gofi-sdk-go/gofi => ../../../../gofi
	github.com/gofi-labs/gofi-sdk-go/iam => ../../../../iam
	github.com/gofi-labs/gofi-sdk-go/msq => ../../../../msq
	github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka => ../../../../msq/provider/kafka
	github.com/gofi-labs/gofi-sdk-go/netx => ../../../../netx
	github.com/gofi-labs/gofi-sdk-go/obs => ../../../../obs
	github.com/gofi-labs/gofi-sdk-go/sqln => ../../../../sqln
)
