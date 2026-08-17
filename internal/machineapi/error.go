package machineapi

import (
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
)

// writeError renders an errorx error as the spec's Error body. errorx
// carries no transport, so this is where a kind becomes a status on
// this listener.
func writeError(w http.ResponseWriter, err error) {
	status := statusOf(errorx.KindOf(err))
	body := machinegen.Error{Kind: errorx.KindOf(err).String(), Message: err.Error()}
	if status >= http.StatusInternalServerError {
		// Do not leak internals on 5xx.
		body.Message = "internal error"
	}
	writeJSON(w, status, body)
}

// statusOf maps error kinds to HTTP status codes. Kinds are
// gRPC-aligned (ADR-0003) and each takes the status of the code it is
// named for, so what a caller must do next is on the status line rather
// than in the kind it would otherwise read out of the body. Only the
// kinds that mean a fault of this process reach the default.
func statusOf(k errorx.Kind) int {
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

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write after WriteHeader cannot be reported to the
	// caller; the server logs at the transport layer.
	_ = json.NewEncoder(w).Encode(v)
}
