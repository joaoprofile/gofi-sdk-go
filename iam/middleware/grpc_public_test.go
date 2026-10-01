package middleware

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestAuthInterceptor_HealthIsPublic(t *testing.T) {
	called := false
	_, err := AuthInterceptor(nil)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"},
		func(context.Context, any) (any, error) { called = true; return nil, nil })
	if err != nil || !called {
		t.Fatalf("health check must bypass auth: called=%v err=%v", called, err)
	}
}

// Reflection used to be public and hardcoded: it lists every service to anyone.
func TestAuthInterceptor_ReflectionIsOptIn(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"}
	ok := func(context.Context, any) (any, error) { return nil, nil }
	if _, err := AuthInterceptor(nil)(context.Background(), nil, info, ok); err == nil {
		t.Fatal("reflection must require a token by default")
	}
	if _, err := AuthInterceptor(nil, WithPublicReflection())(context.Background(), nil, info, ok); err != nil {
		t.Fatalf("WithPublicReflection must open it: %v", err)
	}
	stream := &grpc.StreamServerInfo{FullMethod: info.FullMethod}
	okStream := func(any, grpc.ServerStream) error { return nil }
	ss := &mockServerStream{ctx: context.Background()}
	if err := AuthStreamInterceptor(nil)(nil, ss, stream, okStream); err == nil {
		t.Fatal("streaming reflection must require a token by default")
	}
	if err := AuthStreamInterceptor(nil, WithPublicReflection())(nil, nil, stream, okStream); err != nil {
		t.Fatalf("WithPublicReflection must open streaming reflection: %v", err)
	}
	info.FullMethod = "/pkg.Public/Ping"
	if _, err := AuthInterceptor(nil, WithPublicMethods("/pkg.Public/"))(context.Background(), nil, info, ok); err != nil {
		t.Fatalf("WithPublicMethods: %v", err)
	}
}

func TestAuthInterceptor_OtherMethodsRequireToken(t *testing.T) {
	_, err := AuthInterceptor(nil)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/orders.v1.Orders/Get"},
		func(context.Context, any) (any, error) { return nil, nil })
	if err == nil {
		t.Fatal("expected Unauthenticated without metadata")
	}
}
