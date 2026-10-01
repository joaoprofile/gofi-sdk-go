package middleware

import (
	"context"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCOption configures the auth interceptors.
type GRPCOption func(*grpcOptions)

type grpcOptions struct {
	publicPrefixes []string
}

// WithPublicReflection lets server reflection through without a token. Off by
// default: reflection lists every service and method to anyone.
func WithPublicReflection() GRPCOption {
	return WithPublicMethods("/grpc.reflection.")
}

// WithPublicMethods lets full method names with any of the prefixes through
// without a token (e.g. "/pkg.Service/Method" or "/pkg.Service/").
func WithPublicMethods(prefixes ...string) GRPCOption {
	return func(o *grpcOptions) { o.publicPrefixes = append(o.publicPrefixes, prefixes...) }
}

func newGRPCOptions(opts []GRPCOption) *grpcOptions {
	// Health checks (Kubernetes gRPC probes) are always public.
	o := &grpcOptions{publicPrefixes: []string{"/grpc.health.v1.Health/"}}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *grpcOptions) isPublic(fullMethod string) bool {
	for _, p := range o.publicPrefixes {
		if strings.HasPrefix(fullMethod, p) {
			return true
		}
	}
	return false
}

// AuthInterceptor is the gRPC equivalent of AuthMiddleware for unary RPCs.
// Validates the token from the "authorization: Bearer" metadata entry.
// Injects the claims into the context for use in gRPC handlers.
// Only health checks are public unless opts add more (see WithPublicReflection).
func AuthInterceptor(svc *core.IAMService, opts ...GRPCOption) grpc.UnaryServerInterceptor {
	o := newGRPCOptions(opts)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if o.isPublic(info.FullMethod) {
			return handler(ctx, req)
		}
		ctx, err := authenticateGRPC(ctx, svc)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor is the gRPC equivalent of AuthMiddleware for streaming RPCs.
func AuthStreamInterceptor(svc *core.IAMService, opts ...GRPCOption) grpc.StreamServerInterceptor {
	o := newGRPCOptions(opts)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if o.isPublic(info.FullMethod) {
			return handler(srv, ss)
		}
		ctx, err := authenticateGRPC(ss.Context(), svc)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ss, ctx})
	}
}

// authenticateGRPC extracts and validates the token from gRPC metadata.
func authenticateGRPC(ctx context.Context, svc *core.IAMService) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization")
	}

	token := extractBearerFromValue(values[0])
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
	}

	claims, err := svc.ValidateToken(ctx, token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	return context.WithValue(ctx, contextKeyClaims, claims), nil
}

// extractBearerFromValue extracts the token value from a "Bearer <token>" string.
func extractBearerFromValue(v string) string {
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// wrappedStream wraps grpc.ServerStream replacing its context.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}
