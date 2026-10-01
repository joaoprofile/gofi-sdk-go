module github.com/gofi-labs/gofi-sdk-go/examples/msq/sqs/consumer

go 1.26.6

require (
	github.com/gofi-labs/gofi-sdk-go/gofi v0.8.2
	github.com/gofi-labs/gofi-sdk-go/msq v0.8.2
	github.com/gofi-labs/gofi-sdk-go/msq/provider/sqs v0.8.2
	github.com/gofi-labs/gofi-sdk-go/obs v0.8.2
)

require (
	github.com/aws/aws-sdk-go-v2 v1.47.1 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.33.6 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.20.6 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.52.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.1 // indirect
	github.com/aws/smithy-go v1.28.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/base v0.8.2 // indirect
	github.com/gofi-labs/gofi-sdk-go/base/cloud/aws v0.8.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../../../../base
	github.com/gofi-labs/gofi-sdk-go/base/cloud/aws => ../../../../base/cloud/aws
	github.com/gofi-labs/gofi-sdk-go/gofi => ../../../../gofi
	github.com/gofi-labs/gofi-sdk-go/iam => ../../../../iam
	github.com/gofi-labs/gofi-sdk-go/msq => ../../../../msq
	github.com/gofi-labs/gofi-sdk-go/msq/provider/sqs => ../../../../msq/provider/sqs
	github.com/gofi-labs/gofi-sdk-go/netx => ../../../../netx
	github.com/gofi-labs/gofi-sdk-go/obs => ../../../../obs
	github.com/gofi-labs/gofi-sdk-go/sqln => ../../../../sqln
)
