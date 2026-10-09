// Command grpc-server serves people.v1.PeopleService over gRPC only, on :9090.
package main

import (
	"log"

	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/grpcapi"
	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/people"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/grpcserver"
	"github.com/joaoprofile/gofi-sdk-go/netx/grpcx"
)

func main() {
	store := people.NewStore()

	svc, err := gofi.New("people-grpc").
		With(
			// TLS comes from GRPC_TLS_CERT_FILE / GRPC_TLS_KEY_FILE when set;
			// without them it serves plaintext h2c (fine in dev, refused in
			// prod/stage with GRPC_REQUIRE_TLS=true).
			grpcserver.New(":9090", &grpcx.ServerConfig{
				Auth:       grpcapi.AuthConfig(),
				Reflection: true, // lets grpcurl list and describe the API
			}).Register(grpcapi.Register(store)),
		).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// Blocks until SIGINT/SIGTERM, reports NOT_SERVING, drains and stops.
	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
