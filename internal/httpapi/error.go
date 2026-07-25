package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/errorx"
)

// writeError renders an errorx error as the spec's Error body. The
// HTTP status is the wire mapping of the error kind; errorx stays
// transport-agnostic, this function is the mapping point.
func writeError(w http.ResponseWriter, err error) {
	status := statusOf(errorx.KindOf(err))
	body := api.Error{Kind: errorx.KindOf(err).String(), Message: err.Error()}
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
