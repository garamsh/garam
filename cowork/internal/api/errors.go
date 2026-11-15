package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	apigen "github.com/garamsh/garam/cowork/api"
	"github.com/garamsh/garam/cowork/internal/policyfeed"
	"github.com/garamsh/garam/lib/errorx"
)

// statusOf is the one kind-to-status table of cowork's HTTP surface:
// every handler answers an error through writeError, and nothing else
// names a status for one.
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
	case errorx.KindUnavailable:
		return http.StatusServiceUnavailable
	case errorx.KindDeadlineExceeded:
		return http.StatusGatewayTimeout
	case errorx.KindUnimplemented:
		return http.StatusNotImplemented
	}
	return http.StatusInternalServerError
}

// writeError answers err by its kind. A 5xx names no cause: the cause
// is logged here, once, and the caller reads the status. The one 5xx
// that is answered whole is a create whose entity is stored and not
// yet held by the policy service (ADR-0166): its reason and the stored
// entity's GRN are what let the caller read the entity and not create
// it again, and nothing in it is a cause of cowork's own.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	kind := errorx.KindOf(err)
	status := statusOf(kind)
	message := err.Error()
	if reason, ok := errorx.ReasonOf(err); ok && kind == errorx.KindUnavailable &&
		reason.Code == policyfeed.ReasonChangeNotApplied {
		body := apigen.Error{Kind: kind.String(), Message: message, Reason: &reason.Code}
		var stored interface{ Resource() string }
		if errors.As(err, &stored) {
			resource := stored.Resource()
			body.Resource = &resource
		}
		writeJSON(w, status, body)
		return
	}
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "cowork: request failed",
			slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("error", message))
		message = "internal error"
	}
	writeJSON(w, status, apigen.Error{Kind: kind.String(), Message: message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// BindingError answers a request the generated router could not bind,
// a malformed path parameter among them, as invalid.
func BindingError(w http.ResponseWriter, r *http.Request, err error) {
	writeError(w, r, errorx.Wrap(err, "request", errorx.KindInvalidArgument))
}
