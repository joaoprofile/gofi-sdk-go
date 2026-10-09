// Command http-grpc-server is one gofi service with two servers over the same
// store: gRPC on :9090 (people.v1.PeopleService) and HTTP on :8080 (JSON reads
// and the photos uploaded through gRPC). ListenAndServe runs both and stops
// both on SIGINT/SIGTERM; if either fails, the other is stopped too.
package main

import (
	"log"

	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/grpcapi"
	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/httpapi"
	"github.com/joaoprofile/gofi-sdk-go/examples/grpc/people"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/grpcserver"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/httpserver"
	"github.com/joaoprofile/gofi-sdk-go/netx/grpcx"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

func main() {
	store := people.NewStore() // shared: written over gRPC, read over both

	svc, err := gofi.New("people").
		With(
			grpcserver.New(":9090", &grpcx.ServerConfig{
				Auth:       grpcapi.AuthConfig(),
				Reflection: true,
			}).Register(grpcapi.Register(store)),

			httpserver.New(":8080", &httpx.WSConfig{
				Health: &httpx.HealthConfig{}, // /livez and /readyz
			}).Handlers(httpapi.NewHandler(store)),
		).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
