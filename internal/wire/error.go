package wire

import (
	"net/http"

	"google.golang.org/grpc/codes"

	"github.com/garamsh/garam/internal/errorx"
)

// StatusOf maps error kinds to HTTP status codes. Kinds are
// gRPC-aligned (ADR-0003) and each takes the status of the code it is
// named for, so what a caller must do next is on the status line rather
// than in the kind it would otherwise read out of the body. Only the
// kinds that mean a fault of this process reach the default.
func StatusOf(k errorx.Kind) int {
	switch k {
	case errorx.KindInvalidArgument, errorx.KindOutOfRange:
		return http.StatusBadRequest
	case errorx.KindUnauthenticated:
		return http.StatusUnauthorized
	case errorx.KindPermissionDenied:
		return http.StatusForbidden
	case errorx.KindNotFound:
		return http.StatusNotFound
	case errorx.KindAlreadyExists, errorx.KindFailedPrecondition, errorx.KindAborted:
		return http.StatusConflict
	case errorx.KindResourceExhausted:
		return http.StatusTooManyRequests
	case errorx.KindUnimplemented:
		return http.StatusNotImplemented
	case errorx.KindUnavailable, errorx.KindCanceled:
		// Transient: the call did not complete, so a caller holding a
		// durable queue retries rather than drops.
		return http.StatusServiceUnavailable
	case errorx.KindDeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		// KindUnknown, KindInternal, KindDataLoss, and any kind no
		// constant names.
		return http.StatusInternalServerError
	}
}

// KindOf maps gRPC status codes back to error kinds by name.
func KindOf(c codes.Code) errorx.Kind {
	switch c {
	case codes.Canceled:
		return errorx.KindCanceled
	case codes.InvalidArgument:
		return errorx.KindInvalidArgument
	case codes.DeadlineExceeded:
		return errorx.KindDeadlineExceeded
	case codes.NotFound:
		return errorx.KindNotFound
	case codes.AlreadyExists:
		return errorx.KindAlreadyExists
	case codes.PermissionDenied:
		return errorx.KindPermissionDenied
	case codes.ResourceExhausted:
		return errorx.KindResourceExhausted
	case codes.FailedPrecondition:
		return errorx.KindFailedPrecondition
	case codes.Aborted:
		return errorx.KindAborted
	case codes.OutOfRange:
		return errorx.KindOutOfRange
	case codes.Unimplemented:
		return errorx.KindUnimplemented
	case codes.Internal:
		return errorx.KindInternal
	case codes.Unavailable:
		return errorx.KindUnavailable
	case codes.DataLoss:
		return errorx.KindDataLoss
	case codes.Unauthenticated:
		return errorx.KindUnauthenticated
	default:
		return errorx.KindUnknown
	}
}
