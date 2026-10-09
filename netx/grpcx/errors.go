package grpcx

import (
	"context"
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/joaoprofile/gofi-sdk-go/base/errs"
)

// ErrorDomain is the ErrorInfo domain grpcx uses to carry an AppError.
const ErrorDomain = "gofi.errs"

const kindMetadataKey = "kind"

// internalMessage replaces the message of errors that are not AppError, so
// causes never leak to callers.
const internalMessage = "internal error"

// CodeOf maps an AppError kind to the gRPC status code, following the HTTP
// status httpx.RespondError uses for the same kind.
func CodeOf(kind errs.ErrorKind) codes.Code {
	switch kind {
	case errs.KindValidation:
		return codes.InvalidArgument
	case errs.KindNotFound:
		return codes.NotFound
	case errs.KindConflict:
		return codes.AlreadyExists
	case errs.KindUnauthorized:
		return codes.Unauthenticated
	case errs.KindForbidden:
		return codes.PermissionDenied
	case errs.KindExternalError:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}

// kindOf is the inverse of CodeOf, used when a status carries no ErrorInfo.
func kindOf(code codes.Code) errs.ErrorKind {
	switch code {
	case codes.InvalidArgument, codes.OutOfRange:
		return errs.KindValidation
	case codes.NotFound:
		return errs.KindNotFound
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		return errs.KindConflict
	case codes.Unauthenticated:
		return errs.KindUnauthorized
	case codes.PermissionDenied:
		return errs.KindForbidden
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.KindExternalError
	default:
		return errs.KindOperation
	}
}

// Status converts err into a gRPC status error. A status error passes
// through; context cancellation and deadline map to Canceled and
// DeadlineExceeded; an AppError becomes CodeOf(kind) with its message and an
// ErrorInfo detail carrying its code and kind, never its cause; any other
// error becomes Internal with a generic message. A nil err returns nil.
func Status(err error) error {
	if err == nil {
		return nil
	}
	var appErr errs.AppError
	if errors.As(err, &appErr) {
		if !appErr.Exists() {
			return nil
		}
		return appErrorStatus(appErr).Err()
	}
	var ptrErr *errs.AppError
	if errors.As(err, &ptrErr) && ptrErr != nil {
		if !ptrErr.Exists() {
			return nil
		}
		return appErrorStatus(*ptrErr).Err()
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, context.Canceled.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, context.DeadlineExceeded.Error())
	}
	return status.Error(codes.Internal, internalMessage)
}

func appErrorStatus(e errs.AppError) *status.Status {
	st := status.New(CodeOf(e.Kind), e.Message)
	withInfo, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   e.Code,
		Domain:   ErrorDomain,
		Metadata: map[string]string{kindMetadataKey: string(e.Kind)},
	})
	if err != nil {
		return st
	}
	return withInfo
}

// FromError converts an error returned by a gRPC call into an AppError. A
// status carrying a grpcx ErrorInfo yields the AppError registered under that
// code in this process (errs.GetErrorByCode) or, when the code is unknown
// here, one rebuilt from the kind and message. Other statuses get the kind
// that matches their code. ok is false when err is nil.
func FromError(err error) (errs.AppError, bool) {
	if err == nil {
		return errs.AppError{}, false
	}
	st, isStatus := status.FromError(err)
	if !isStatus {
		return errs.AppError{Kind: errs.KindOperation, Code: codes.Unknown.String(), Message: err.Error(), Err: err}, true
	}
	for _, d := range st.Details() {
		info, ok := d.(*errdetails.ErrorInfo)
		if !ok || info.GetDomain() != ErrorDomain {
			continue
		}
		if registered := errs.GetErrorByCode(info.GetReason()); registered != nil && registered.Code != "" {
			e := *registered
			e.Message = st.Message()
			e.Err = err
			return e, true
		}
		kind := errs.ErrorKind(info.GetMetadata()[kindMetadataKey])
		if kind == "" {
			kind = kindOf(st.Code())
		}
		return errs.AppError{Kind: kind, Code: info.GetReason(), Message: st.Message(), Err: err}, true
	}
	return errs.AppError{Kind: kindOf(st.Code()), Code: st.Code().String(), Message: st.Message(), Err: err}, true
}
