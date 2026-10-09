package grpcx

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/joaoprofile/gofi-sdk-go/netx"
)

// --- a hand-written server-streaming service ---

type streamFunc func(ss grpc.ServerStream) error

const repeatMethod = "/test.Stream/Repeat"

var streamDesc = grpc.ServiceDesc{
	ServiceName: "test.Stream",
	HandlerType: (*any)(nil),
	Streams: []grpc.StreamDesc{{
		StreamName:    "Repeat",
		ServerStreams: true,
		Handler:       func(srv any, ss grpc.ServerStream) error { return srv.(streamFunc)(ss) },
	}},
}

func startStreamServer(t *testing.T, cfg ServerConfig, impl streamFunc) string {
	t.Helper()
	_, addr := startServerWith(t, cfg, func(srv Server) { srv.RegisterService(&streamDesc, impl) })
	return addr
}

// callRepeat sends msg and collects every message the server streams back.
func callRepeat(ctx context.Context, conn *grpc.ClientConn, msg string) ([]string, metadata.MD, error) {
	cs, err := conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, repeatMethod)
	if err != nil {
		return nil, nil, err
	}
	if err := cs.SendMsg(wrapperspb.String(msg)); err != nil {
		return nil, nil, err
	}
	if err := cs.CloseSend(); err != nil {
		return nil, nil, err
	}
	var got []string
	for {
		out := new(wrapperspb.StringValue)
		err := cs.RecvMsg(out)
		if errors.Is(err, io.EOF) {
			return got, cs.Trailer(), nil
		}
		if err != nil {
			return got, cs.Trailer(), err
		}
		got = append(got, out.GetValue())
	}
}

// repeat streams back the message, the request ID and the caller.
var repeat = streamFunc(func(ss grpc.ServerStream) error {
	in := new(wrapperspb.StringValue)
	if err := ss.RecvMsg(in); err != nil {
		return err
	}
	caller, _ := ss.Context().Value(callerKey{}).(string)
	for _, v := range []string{in.GetValue(), netx.GetRequestID(ss.Context()), caller} {
		if err := ss.SendMsg(wrapperspb.String(v)); err != nil {
			return err
		}
	}
	return nil
})

func goodTokenAuth(public ...string) *AuthConfig {
	return &AuthConfig{
		Validate: func(ctx context.Context, token string) (context.Context, error) {
			if token != "good" {
				return nil, errors.New("bad token")
			}
			return context.WithValue(ctx, callerKey{}, "svc-a"), nil
		},
		PublicMethods: public,
	}
}

func TestStream_AuthRequestIDAndContext(t *testing.T) {
	addr := startStreamServer(t, ServerConfig{Auth: goodTokenAuth()}, repeat)
	conn := dial(t, addr, ClientConfig{Token: func(context.Context) (string, error) { return "good", nil }})

	ctx := context.WithValue(context.Background(), netx.RequestIDKey, "req-stream")
	got, trailer, err := callRepeat(ctx, conn, "hi")
	require.NoError(t, err)
	assert.Equal(t, []string{"hi", "req-stream", "svc-a"}, got,
		"the client forwards the request ID and the validator context reaches the handler")
	assert.Equal(t, []string{"req-stream"}, trailer.Get(RequestIDMetadata))
}

func TestStream_Unauthenticated(t *testing.T) {
	addr := startStreamServer(t, ServerConfig{Auth: goodTokenAuth()}, repeat)
	_, _, err := callRepeat(context.Background(), dial(t, addr, ClientConfig{}), "hi")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, unauthenticatedMessage, status.Convert(err).Message())
}

func TestStream_PublicMethod(t *testing.T) {
	addr := startStreamServer(t, ServerConfig{Auth: goodTokenAuth("/test.Stream/")}, repeat)
	got, _, err := callRepeat(context.Background(), dial(t, addr, ClientConfig{}), "hi")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "hi", got[0])
	assert.NotEmpty(t, got[1], "a missing request ID is generated")
	assert.Empty(t, got[2], "public methods skip the validator")
}

func TestStream_RecoversFromPanic(t *testing.T) {
	addr := startStreamServer(t, ServerConfig{}, func(grpc.ServerStream) error { panic("boom") })
	_, _, err := callRepeat(context.Background(), dial(t, addr, ClientConfig{}), "hi")
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, internalMessage, status.Convert(err).Message())
}

func TestStream_ErrorMapping(t *testing.T) {
	addr := startStreamServer(t, ServerConfig{}, func(grpc.ServerStream) error {
		return errTestNotFound.Wrap(errors.New("db: no rows"))
	})
	conn := dial(t, addr, ClientConfig{})
	_, _, err := callRepeat(context.Background(), conn, "hi")
	assert.Equal(t, codes.NotFound, status.Code(err))
	appErr, ok := FromError(err)
	require.True(t, ok)
	assert.Equal(t, "GRPCX_TEST_NOT_FOUND", appErr.Code)

	plain := startStreamServer(t, ServerConfig{}, func(grpc.ServerStream) error { return errors.New("secret") })
	_, _, err = callRepeat(context.Background(), dial(t, plain, ClientConfig{}), "hi")
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, err.Error(), "secret")
}

func TestStream_CustomInterceptors(t *testing.T) {
	var serverSaw, clientSaw string
	addr := startStreamServer(t, ServerConfig{StreamInterceptors: []grpc.StreamServerInterceptor{
		func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			serverSaw = info.FullMethod
			return handler(srv, ss)
		},
	}}, repeat)
	conn := dial(t, addr, ClientConfig{StreamInterceptors: []grpc.StreamClientInterceptor{
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			clientSaw = method
			return streamer(ctx, desc, cc, method, opts...)
		},
	}})
	_, _, err := callRepeat(context.Background(), conn, "hi")
	require.NoError(t, err)
	assert.Equal(t, repeatMethod, serverSaw)
	assert.Equal(t, repeatMethod, clientSaw)
}
