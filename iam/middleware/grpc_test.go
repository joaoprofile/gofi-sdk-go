package middleware

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/provider/memory"
	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func buildGRPCService(auth port.AuthPort) *core.IAMService {
	return core.NewService(core.ServiceConfig{
		Auth:    auth,
		Tenant:  &stubTenantPort{},
		RBAC:    &stubRBACPort{},
		Session: memory.NewTestProvider(),
		IDPs:    make(map[string]*core.IDPService),
	})
}

func contextWithMD(pairs ...string) context.Context {
	md := metadata.Pairs(pairs...)
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestAuthInterceptor_Success(t *testing.T) {
	claims := &types.Claims{UserID: "u1"}
	auth := &stubAuthPort{claims: claims}
	svc := buildGRPCService(auth)

	interceptor := AuthInterceptor(svc)
	ctx := contextWithMD("authorization", "Bearer valid-token")

	var receivedClaims *types.Claims
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		receivedClaims = ClaimsFromContext(ctx)
		return "ok", nil
	})

	require.NoError(t, err)
	require.NotNil(t, receivedClaims)
	assert.Equal(t, "u1", receivedClaims.UserID)
}

func TestAuthInterceptor_NoMetadata(t *testing.T) {
	svc := buildGRPCService(&stubAuthPort{})

	interceptor := AuthInterceptor(svc)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuthInterceptor_MissingAuthHeader(t *testing.T) {
	svc := buildGRPCService(&stubAuthPort{})

	interceptor := AuthInterceptor(svc)
	ctx := contextWithMD("other-key", "value")

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, nil)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuthInterceptor_InvalidBearerFormat(t *testing.T) {
	svc := buildGRPCService(&stubAuthPort{})

	interceptor := AuthInterceptor(svc)
	ctx := contextWithMD("authorization", "Basic dXNlcjpwYXNz")

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, nil)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	auth := &stubAuthPort{validateErr: core.ErrTokenInvalid}
	svc := buildGRPCService(auth)

	interceptor := AuthInterceptor(svc)
	ctx := contextWithMD("authorization", "Bearer bad-token")

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, nil)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuthInterceptor_BearerCaseInsensitive(t *testing.T) {
	claims := &types.Claims{UserID: "u1"}
	auth := &stubAuthPort{claims: claims}
	svc := buildGRPCService(auth)

	interceptor := AuthInterceptor(svc)
	ctx := contextWithMD("authorization", "BEARER valid-token")

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(_ context.Context, _ any) (any, error) {
		return nil, nil
	})
	assert.NoError(t, err)
}

func TestExtractBearerFromValue_Valid(t *testing.T) {
	assert.Equal(t, "my-token", extractBearerFromValue("Bearer my-token"))
}

func TestExtractBearerFromValue_LowerCase(t *testing.T) {
	assert.Equal(t, "my-token", extractBearerFromValue("bearer my-token"))
}

func TestExtractBearerFromValue_WrongScheme(t *testing.T) {
	assert.Equal(t, "", extractBearerFromValue("Basic dXNlcjpwYXNz"))
}

func TestExtractBearerFromValue_SingleWord(t *testing.T) {
	assert.Equal(t, "", extractBearerFromValue("justtoken"))
}

func TestExtractBearerFromValue_Empty(t *testing.T) {
	assert.Equal(t, "", extractBearerFromValue(""))
}

// mockServerStream is a minimal implementation of grpc.ServerStream for testing.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

func TestAuthStreamInterceptor_Success(t *testing.T) {
	claims := &types.Claims{UserID: "stream-user"}
	auth := &stubAuthPort{claims: claims}
	svc := buildGRPCService(auth)

	interceptor := AuthStreamInterceptor(svc)
	ctx := contextWithMD("authorization", "Bearer valid-token")
	stream := &mockServerStream{ctx: ctx}

	var receivedClaims *types.Claims
	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(_ any, ss grpc.ServerStream) error {
		receivedClaims = ClaimsFromContext(ss.Context())
		return nil
	})

	require.NoError(t, err)
	require.NotNil(t, receivedClaims)
	assert.Equal(t, "stream-user", receivedClaims.UserID)
}

func TestAuthStreamInterceptor_InvalidToken(t *testing.T) {
	auth := &stubAuthPort{validateErr: core.ErrTokenInvalid}
	svc := buildGRPCService(auth)

	interceptor := AuthStreamInterceptor(svc)
	ctx := contextWithMD("authorization", "Bearer bad-token")
	stream := &mockServerStream{ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, nil)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuthStreamInterceptor_NoMetadata(t *testing.T) {
	svc := buildGRPCService(&stubAuthPort{})

	interceptor := AuthStreamInterceptor(svc)
	stream := &mockServerStream{ctx: context.Background()}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, nil)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestWrappedStream_Context(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextKeyClaims, &types.Claims{UserID: "u1"})
	ws := &wrappedStream{ctx: ctx}
	assert.Equal(t, ctx, ws.Context())
}
