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
	if status == http.StatusInternalServerError {
		// Do not leak internals on 5xx.
		body.Message = "internal error"
	}
	writeJSON(w, status, body)
}

// statusOf maps error kinds to HTTP status codes.
func statusOf(k errorx.Kind) int {
	switch k {
	case errorx.KindInvalidArgument:
		return http.StatusBadRequest
	case errorx.KindUnauthenticated:
		return http.StatusUnauthorized
	case errorx.KindPermissionDenied:
		return http.StatusForbidden
	case errorx.KindNotFound:
		return http.StatusNotFound
	case errorx.KindAlreadyExists, errorx.KindFailedPrecondition:
		return http.StatusConflict
	default:
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
