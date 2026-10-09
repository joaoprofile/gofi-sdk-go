// Package grpcx serves and calls gRPC services with the same defaults as the
// netx HTTP server: hardened TLS with certificate reload and optional mTLS,
// request IDs, panic recovery, error-only logging, OpenTelemetry, graceful
// shutdown with readiness draining and the standard gRPC health service.
//
// It is a separate module so services that only speak HTTP never link gRPC.
//
// Server:
//
//	srv, err := grpcx.NewServer(grpcx.ServerConfig{Addr: ":9090"})
//	ordersv1.RegisterOrdersServer(srv, ordersImpl) // generated code takes a grpc.ServiceRegistrar
//	err = srv.ListenAndServe()                     // until SIGINT/SIGTERM or Shutdown
//
// Handlers return errs.AppError (or any error); the server turns it into a
// gRPC status whose code follows the AppError kind and whose details carry the
// AppError code, so a grpcx client gets the same AppError back (FromError).
//
// Client:
//
//	conn, err := grpcx.NewClient("dns:///orders.internal:9090", grpcx.ClientConfig{
//	    TLS:   &grpcx.ClientTLSConfig{CAFile: "/etc/tls/ca.pem"},
//	    Token: tokenSource, // optional bearer token per call
//	})
//	defer conn.Close()
//	client := ordersv1.NewOrdersClient(conn)
//
// gofi services declare the server with the gofi/component/grpcserver component.
package grpcx
