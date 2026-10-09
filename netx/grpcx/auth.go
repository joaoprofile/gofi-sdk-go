package grpcx

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenValidator checks the bearer token of a call and returns the context
// the handler runs with (typically carrying the caller's claims). Any error
// rejects the call with Unauthenticated and a generic message; the error is
// never sent to the caller.
type TokenValidator func(ctx context.Context, token string) (context.Context, error)

// AuthConfig enables bearer-token authentication on the server.
type AuthConfig struct {
	// Validate checks the token from the "authorization: Bearer" metadata.
	Validate TokenValidator
	// PublicMethods are full-method prefixes served without a token
	// ("/pkg.Service/Method" or "/pkg.Service/"). Health checks are always
	// public; reflection is public only when listed here.
	PublicMethods []string
}

const unauthenticatedMessage = "unauthenticated"

func (a *AuthConfig) public(method string) bool {
	return isHealthMethod(method) || methodHasPrefix(method, a.PublicMethods)
}

func authUnary(a *AuthConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if a.public(info.FullMethod) {
			return handler(ctx, req)
		}
		ctx, err := authenticate(ctx, a.Validate)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func authStream(a *AuthConfig) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if a.public(info.FullMethod) {
			return handler(srv, ss)
		}
		ctx, err := authenticate(ss.Context(), a.Validate)
		if err != nil {
			return err
		}
		return handler(srv, &contextStream{ServerStream: ss, ctx: ctx})
	}
}

func authenticate(ctx context.Context, validate TokenValidator) (context.Context, error) {
	token, ok := bearerToken(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, unauthenticatedMessage)
	}
	next, err := validate(ctx, token)
	if err != nil || next == nil {
		return nil, status.Error(codes.Unauthenticated, unauthenticatedMessage)
	}
	return next, nil
}

func bearerToken(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// TokenSource returns the bearer token for one call; it is called on every
// RPC, so cache inside it when minting tokens is expensive.
type TokenSource func(ctx context.Context) (string, error)

// bearerCredentials sends "authorization: Bearer <token>" on every call.
type bearerCredentials struct {
	source         TokenSource
	requireSecured bool
}

func (b bearerCredentials) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	token, err := b.source(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "grpcx: token source: %v", err)
	}
	return map[string]string{"authorization": "Bearer " + token}, nil
}

func (b bearerCredentials) RequireTransportSecurity() bool { return b.requireSecured }

// BearerCredentials returns per-RPC credentials that send the token from
// source. requireTLS makes gRPC refuse to send it over a plaintext
// connection; turn it off only inside a private network.
func BearerCredentials(source TokenSource, requireTLS bool) credentials.PerRPCCredentials {
	return bearerCredentials{source: source, requireSecured: requireTLS}
}
