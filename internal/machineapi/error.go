package machineapi

import (
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/wire"
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

// writeError renders an errorx error as the spec's Error body. errorx
// carries no transport; the kind-to-status mapping is the shared one in
// internal/wire.
func writeError(w http.ResponseWriter, err error) {
	status := wire.StatusOf(errorx.KindOf(err))
	body := machinegen.Error{Kind: errorx.KindOf(err).String(), Message: err.Error()}
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

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed write after WriteHeader cannot be reported to the
	// caller; the server logs at the transport layer.
	_ = json.NewEncoder(w).Encode(v)
}
