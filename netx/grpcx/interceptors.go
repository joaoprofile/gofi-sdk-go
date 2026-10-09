package grpcx

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/joaoprofile/gofi-sdk-go/netx"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// RequestIDMetadata carries the request ID in both directions, the gRPC
// counterpart of netx.RequestIDHeader.
const RequestIDMetadata = "x-request-id"

const maxRequestIDLen = 64

// unaryChain is the built-in chain, outermost first: recovery wraps
// everything, the request ID is set before anything logs, logging sees the
// final status, auth runs before error mapping, and the caller's interceptors
// sit closest to the handler.
func unaryChain(c ServerConfig) []grpc.UnaryServerInterceptor {
	chain := []grpc.UnaryServerInterceptor{recoveryUnary, requestIDUnary, loggingUnary}
	if c.Auth != nil {
		chain = append(chain, authUnary(c.Auth))
	}
	chain = append(chain, errorsUnary)
	return append(chain, c.UnaryInterceptors...)
}

func streamChain(c ServerConfig) []grpc.StreamServerInterceptor {
	chain := []grpc.StreamServerInterceptor{recoveryStream, requestIDStream, loggingStream}
	if c.Auth != nil {
		chain = append(chain, authStream(c.Auth))
	}
	chain = append(chain, errorsStream)
	return append(chain, c.StreamInterceptors...)
}

// --- recovery ---

func recoveryUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = panicStatus(ctx, info.FullMethod, p)
		}
	}()
	return handler(ctx, req)
}

func recoveryStream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = panicStatus(ss.Context(), info.FullMethod, p)
		}
	}()
	return handler(srv, ss)
}

func panicStatus(ctx context.Context, method string, p any) error {
	logging.FromContext(ctx).Error("panic in gRPC handler",
		slog.String("method", method),
		slog.String("panic", fmt.Sprint(p)),
		slog.String("stack", string(debug.Stack())),
	)
	return status.Error(codes.Internal, internalMessage)
}

// --- request ID ---

// withRequestID stores the caller's x-request-id when it is 1-64 of
// [A-Za-z0-9._-], a random one otherwise, under netx.RequestIDKey (so
// netx.GetRequestID works in handlers), and echoes it in the response
// trailer. A header would make error responses non-trailers-only, which gRPC
// clients can no longer retry.
func withRequestID(ctx context.Context) (context.Context, string) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(RequestIDMetadata); len(v) > 0 {
			id = v[0]
		}
	}
	if !validRequestID(id) {
		id = rand.Text()
	}
	_ = grpc.SetTrailer(ctx, metadata.Pairs(RequestIDMetadata, id))
	return context.WithValue(ctx, netx.RequestIDKey, id), id
}

func requestIDUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, _ = withRequestID(ctx)
	return handler(ctx, req)
}

func requestIDStream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, _ := withRequestID(ss.Context())
	return handler(srv, &contextStream{ServerStream: ss, ctx: ctx})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// --- logging ---

// Logging follows netx.LoggingMiddleware: successful calls are silent; server
// faults are logged at Error, denied and throttled calls at Warn.
func loggingUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	logCall(ctx, info.FullMethod, err, time.Since(start))
	return resp, err
}

func loggingStream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, ss)
	logCall(ss.Context(), info.FullMethod, err, time.Since(start))
	return err
}

func logCall(ctx context.Context, method string, err error, latency time.Duration) {
	if err == nil || isHealthMethod(method) {
		return
	}
	code := status.Code(err)
	var level slog.Level
	var msg string
	switch code {
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unimplemented:
		level, msg = slog.LevelError, "server error"
	case codes.Unauthenticated, codes.PermissionDenied, codes.ResourceExhausted:
		level, msg = slog.LevelWarn, "access denied"
	default:
		return
	}
	logging.FromContext(ctx).LogAttrs(ctx, level, msg,
		slog.String("request_id", netx.GetRequestID(ctx)),
		slog.String("method", method),
		slog.String("code", code.String()),
		slog.String("peer", peerAddr(ctx)),
		slog.Duration("latency", latency),
	)
}

func peerAddr(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}

// --- error mapping ---

// errorsUnary converts what the handler returns with Status, logging the
// cause of errors that become Internal (the caller never sees it).
func errorsUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	return resp, mapError(ctx, info.FullMethod, err)
}

func errorsStream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return mapError(ss.Context(), info.FullMethod, handler(srv, ss))
}

func mapError(ctx context.Context, method string, err error) error {
	if err == nil {
		return nil
	}
	mapped := Status(err)
	if status.Code(mapped) == codes.Internal && mapped != err {
		logging.FromContext(ctx).Error("gRPC handler error",
			slog.String("request_id", netx.GetRequestID(ctx)),
			slog.String("method", method),
			slog.Any("error", err),
		)
	}
	return mapped
}

// contextStream replaces the context of a server stream.
type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextStream) Context() context.Context { return s.ctx }

// methodHasPrefix reports whether method starts with any of the prefixes.
func methodHasPrefix(method string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(method, p) {
			return true
		}
	}
	return false
}
