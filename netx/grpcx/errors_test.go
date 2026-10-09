package grpcx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/joaoprofile/gofi-sdk-go/base/errs"
)

func TestKindOf(t *testing.T) {
	for code, kind := range map[codes.Code]errs.ErrorKind{
		codes.InvalidArgument: errs.KindValidation, codes.OutOfRange: errs.KindValidation,
		codes.NotFound:      errs.KindNotFound,
		codes.AlreadyExists: errs.KindConflict, codes.Aborted: errs.KindConflict, codes.FailedPrecondition: errs.KindConflict,
		codes.Unauthenticated:  errs.KindUnauthorized,
		codes.PermissionDenied: errs.KindForbidden,
		codes.Unavailable:      errs.KindExternalError, codes.DeadlineExceeded: errs.KindExternalError, codes.ResourceExhausted: errs.KindExternalError,
		codes.Internal: errs.KindOperation, codes.Unknown: errs.KindOperation,
	} {
		assert.Equal(t, kind, kindOf(code), code.String())
	}
}

func TestStatus_AppErrorPointer(t *testing.T) {
	err := Status(&errs.AppError{Kind: errs.KindValidation, Code: "BAD", Message: "bad input"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "bad input", status.Convert(err).Message())
	assert.NoError(t, Status(&errs.AppError{}), "an empty *AppError is success")
}

func TestFromError_Fallbacks(t *testing.T) {
	plain := errors.New("connection reset")
	e, ok := FromError(plain)
	require.True(t, ok)
	assert.Equal(t, errs.KindOperation, e.Kind)
	assert.Equal(t, "connection reset", e.Message)

	st, err := status.New(codes.NotFound, "gone").WithDetails(&errdetails.ErrorInfo{Reason: "NO_KIND", Domain: ErrorDomain})
	require.NoError(t, err)
	e, ok = FromError(st.Err())
	require.True(t, ok)
	assert.Equal(t, "NO_KIND", e.Code)
	assert.Equal(t, errs.KindNotFound, e.Kind, "a missing kind falls back to the status code")

	st, err = status.New(codes.PermissionDenied, "nope").WithDetails(&errdetails.ErrorInfo{Reason: "OTHER", Domain: "example.com"})
	require.NoError(t, err)
	e, ok = FromError(st.Err())
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied.String(), e.Code, "foreign ErrorInfo domains are ignored")
	assert.Equal(t, errs.KindForbidden, e.Kind)
}

func TestAuthenticate(t *testing.T) {
	accept := func(ctx context.Context, _ string) (context.Context, error) { return ctx, nil }
	withAuth := func(v string) context.Context {
		return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", v))
	}

	_, err := authenticate(context.Background(), accept)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "no metadata")
	_, err = authenticate(withAuth("Basic dXNlcjpwYXNz"), accept)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "wrong scheme")
	_, err = authenticate(withAuth("Bearer   "), accept)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "empty token")
	_, err = authenticate(withAuth("Bearer tok"), func(context.Context, string) (context.Context, error) { return nil, nil })
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "a nil context is a rejection")

	_, err = authenticate(withAuth("bearer  tok "), func(ctx context.Context, token string) (context.Context, error) {
		assert.Equal(t, "tok", token)
		return ctx, nil
	})
	assert.NoError(t, err, "the scheme is case-insensitive and the token trimmed")
}

func TestBearerCredentials(t *testing.T) {
	creds := BearerCredentials(func(context.Context) (string, error) { return "t", nil }, true)
	md, err := creds.GetRequestMetadata(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"authorization": "Bearer t"}, md)
	assert.True(t, creds.RequireTransportSecurity())
}

func TestLogCall(t *testing.T) {
	// Exercises every level branch, including a context without peer.
	ctx := context.Background()
	assert.Empty(t, peerAddr(ctx))
	for _, code := range []codes.Code{codes.Internal, codes.Unauthenticated, codes.NotFound} {
		logCall(ctx, echoMethod, status.Error(code, "x"), time.Millisecond)
	}
	logCall(ctx, healthServicePrefix+"Check", status.Error(codes.Internal, "x"), 0)
}

func TestDefaultsHelpers(t *testing.T) {
	assert.Equal(t, 7, orDefaultInt(7, 1))
	assert.Equal(t, 1, orDefaultInt(0, 1))
	assert.Equal(t, "x", cmpOr("x", "y"))
	assert.Equal(t, "y", cmpOr(" ", "y"))
	assert.Equal(t, "0.1s", seconds(100*time.Millisecond))
}
