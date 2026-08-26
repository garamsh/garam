package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/wire"
)

// RecoverPanics answers a handler that panicked. net/http recovers the
// panic and logs the stack itself, but abandons the response instead of
// answering: the caller reads a closed connection and cannot tell a
// defect here from a network fault. Recovering first is what makes the
// answer a 500, and the stack travels in the cause because net/http no
// longer sees the panic to log it.
//
// It answers and does not log (stack-go.md §4). The cause reaches the
// request line through the recorder a handled 5xx already reports on,
// so a panic costs one line and one answer like every other failure.
func RecoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			// A recovered panic carries no kind of its own. It is this
			// process's own defect, which is what KindInternal names.
			writeError(w, errorx.New(errorx.KindInternal,
				fmt.Sprintf("panic: %v\n%s", v, debug.Stack())))
		}()
		next.ServeHTTP(w, r)
	})
}

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
// HTTP status is the wire mapping of the error kind (internal/wire);
// errorx stays transport-agnostic and the mapping is shared by every
// surface.
func writeError(w http.ResponseWriter, err error) {
	status := wire.StatusOf(errorx.KindOf(err))
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
