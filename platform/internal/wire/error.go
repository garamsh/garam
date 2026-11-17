package wire

import (
	"net/http"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
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

// MessageOf is what a client may be told about err. An error this
// project constructed through errorx carries its own message; one
// whose chain reaches an error from outside — a driver error naming a
// table, a constraint and a SQLSTATE, a decoder quoting the body it
// could not read — carries text nobody here wrote for a caller, so the
// client reads this project's own sentence for the kind instead. The
// chain is not changed; what it replaced reaches the request log
// instead, at whatever status, because the surface records the chain
// wherever [IsAuthored] is false.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}
	if IsAuthored(err) {
		return err.Error()
	}
	return messageOfKind(errorx.KindOf(err))
}

// IsAuthored reports whether every error in the chain was constructed
// here, through errorx. The type of each link decides it, never the
// text: a message that has to be read to be judged is one that will be
// misjudged, and a driver's message reads like prose. It is also what
// a surface asks to know whether the client's body carried the chain.
func IsAuthored(err error) bool {
	for err != nil {
		e, ok := err.(errorx.Error)
		if !ok {
			return false
		}
		err = e.Unwrap()
	}
	return true
}

// messageOfKind is what a client is told when the chain's text is not
// this project's to show. It says what the kind means for the request
// and nothing about what produced it.
func messageOfKind(k errorx.Kind) string {
	switch k {
	case errorx.KindInvalidArgument, errorx.KindOutOfRange:
		return "the request is not in the required form"
	case errorx.KindUnauthenticated:
		return "unauthenticated"
	case errorx.KindPermissionDenied:
		return "permission denied"
	case errorx.KindNotFound:
		return "not found"
	case errorx.KindAlreadyExists:
		return "already exists"
	case errorx.KindFailedPrecondition:
		return "the request does not apply to the current state"
	case errorx.KindAborted:
		return "the request conflicted with another and was not applied"
	case errorx.KindResourceExhausted:
		return "too many requests"
	case errorx.KindDeadlineExceeded:
		return "the request took too long"
	case errorx.KindUnimplemented:
		return "not implemented"
	default:
		// The kinds that mean a fault of this process. Every surface
		// replaces the message on a 5xx anyway, so this is what one
		// would read if a surface stopped doing that.
		return "internal error"
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

// ErrorOf renders an error a gRPC call returned as an errorx error, its
// status code carrying the kind by name (ADR-0003) and an ErrorInfo
// detail of this domain carrying the reason.
//
// The message is taken as this project's own, which is what the policy
// service undertakes to send: it decides what a caller may be told,
// because a status crossing the hop carries text and nothing that says
// who wrote it. A status the service did not answer at all — one gRPC
// itself raised for a call that never reached a handler — carries
// gRPC's text under that undertaking, and every code it raises but two
// is replaced on the way out as a 5xx.
func ErrorOf(err error) error {
	st, _ := status.FromError(err)
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetDomain() == reasonDomain {
			return errorx.NewWithReason(KindOf(st.Code()),
				errorx.Reason{Code: info.GetReason(), Path: info.GetMetadata()[reasonPathKey]}, st.Message())
		}
	}
	return errorx.New(KindOf(st.Code()), st.Message())
}

// The ErrorInfo a reason crosses a gRPC hop as: the reason by name, in
// this domain, with its JSON Pointer under one metadata key.
const (
	reasonDomain  = "garam.sh"
	reasonPathKey = "path"
)
