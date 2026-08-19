package api

import (
	"encoding/json"
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
)

// causeRecorder is the request-logging middleware's response writer,
// which records the cause of a 5xx so the line it emits for the
// request carries it (runtime-safety.md §Errors reach a handler). The
// seam is declared here rather than imported because the surfaces
// never import the composition root (ADR-0003); a writer without it —
// a test's recorder is one — keeps the cause to itself.
type causeRecorder interface {
	RecordCause(err error)
}

// writeError renders an errorx error as the spec's Error body. The
// HTTP status is the wire mapping of the error kind; errorx stays
// transport-agnostic, this function is the mapping point.
func writeError(w http.ResponseWriter, err error) {
	status := statusOf(errorx.KindOf(err))
	body := apigen.Error{Kind: errorx.KindOf(err).String(), Message: err.Error()}
	if status >= http.StatusInternalServerError {
		// Do not leak internals on 5xx. The chain — which can carry a
		// GRN or a driver error's text, never a row value — reaches
		// the request log instead of the client.
		body.Message = "internal error"
		if recorder, ok := w.(causeRecorder); ok {
			recorder.RecordCause(err)
		}
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

// writeJSON encodes v as JSON with the given status. A status of 204
// writes no body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write after WriteHeader cannot be reported to the
	// client; the server logs at the transport layer.
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON parses a JSON request body into v.
func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errorx.Wrap(err, "request body", errorx.KindInvalidArgument)
	}
	return nil
}
