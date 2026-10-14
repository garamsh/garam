package machineapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/wire"
)

// RecoverPanics answers a handler that panicked with a 500, which
// net/http does not do on its own (stack-go.md §4). The caller here
// holds a durable queue, so an abandoned response is a closed
// connection it retries forever. It answers and does not log: the stack
// travels in the cause, and reaches the request line through the
// recorder a handled 5xx already reports on.
//
// Each surface keeps its own rather than sharing one: the answer is
// that surface's Error, written by its writeError, the one place it
// translates an error (stack-go.md §4), and the two never import each
// other (ADR-0017).
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

// BindingError answers a request the generated router refused while
// binding its parameters, before any handler ran. It replaces the
// router's own default, which writes the binding error's text as
// text/plain and repeats what the caller submitted — neither of which
// this surface's Error response admits.
//
// It names no parameter, where the browser surface's counterpart does:
// every parameter api/machine.yaml declares but one is a path string,
// so there is no format for one to fail and nothing to tell a caller
// beyond that a parameter could not be read. The one is the contract
// version header, and a missing one is answered as an unsupported one
// so the refusal names the value the request's credential takes there.
//
// The composition root supplies this as ErrorHandlerFunc, and nothing
// else answers on this path.
func BindingError(w http.ResponseWriter, r *http.Request, err error) {
	var header *machinegen.RequiredHeaderError
	if errors.As(err, &header) && header.ParamName == operator.ContractVersionHeader {
		switch {
		case executionRoute(r):
			served(w, r, "", executionValues)
		case enrollmentRoute(r):
			served(w, r, "", enrollmentValues)
		case standaloneRoute(r):
			standaloneContract(w, "")
		case cutoverRoute(r):
			served(w, r, "", cutoverValues)
		case r.URL.Path == "/outbox-messages":
			served(w, r, "", deliveryValues)
		case tokenRoute(r):
			w.Header().Set("Garam-Contract-Version", operator.TokenContractVersion)
			writeError(w, operator.ErrUnsupportedTokenContract)
		default:
			served(w, r, "", authorityValues)
		}
		return
	}
	writeError(w, errorx.New(errorx.KindInvalidArgument, "a request parameter could not be read"))
}

// executionRoute reports whether r is one of the execution routes,
// whose contract is execution-fence rather than the operation
// authority's. Every one of them is a POST under /agents/.
func executionRoute(r *http.Request) bool {
	p := r.URL.Path
	return strings.HasPrefix(p, "/agents/") && (strings.HasSuffix(p, "/activations") ||
		strings.HasSuffix(p, "/deactivation") || strings.HasSuffix(p, "/execution/introspection") ||
		strings.HasSuffix(p, "/credential-recovery"))
}

// enrollmentRoute reports whether r is one of the managed enrollment
// routes, whose contract is managed-enrollment. Both are POSTs.
func enrollmentRoute(r *http.Request) bool {
	p := r.URL.Path
	return (strings.HasPrefix(p, "/operators/") && strings.HasSuffix(p, "/managed-agents")) ||
		(strings.HasPrefix(p, "/agents/") && strings.HasSuffix(p, "/initial-certificate"))
}

// cutoverRoute reports whether r is one of the cutover routes, whose
// contract is agent-cutover: the read of an agent's cutover and the
// three stages under it.
func cutoverRoute(r *http.Request) bool {
	p := r.URL.Path
	return strings.HasPrefix(p, "/agents/") && (strings.HasSuffix(p, "/cutover") ||
		strings.HasSuffix(p, "/cutover/freeze") || strings.HasSuffix(p, "/cutover/switch") ||
		strings.HasSuffix(p, "/cutover/rollback"))
}

// standaloneRoute reports whether r is one of the standalone
// enrollment routes, whose contract is standalone-enrollment.v1.
func standaloneRoute(r *http.Request) bool {
	p := r.URL.Path
	return p == "/agent-enrollments" || p == "/supervisors/self/certificate"
}

// causeRecorder is the request-logging middleware's response writer,
// which records the cause the answer withheld so the line it emits
// for the request carries it (runtime-safety.md §Errors reach a
// handler). The seam is declared here rather than imported because
// the surfaces never import the composition root (ADR-0003); a writer
// without it — a test's recorder is one — keeps the cause to itself.
type causeRecorder interface {
	RecordCause(err error)
}

// writeError renders an errorx error as the spec's Error body. errorx
// carries no transport; the kind-to-status mapping is the shared one in
// internal/wire.
func writeError(w http.ResponseWriter, err error) {
	writeErrorStatus(w, wire.StatusOf(errorx.KindOf(err)), err)
}

// writeErrorStatus renders err as writeError does, at a status the
// route chose over the shared mapping.
func writeErrorStatus(w http.ResponseWriter, status int, err error) {
	writeErrorBody(w, status, err, nil)
}

// writeErrorReason renders err as writeErrorStatus does, naming the
// reason the route's contract lists for it (ADR-0086). Only a route
// whose contract lists reasons calls it.
func writeErrorReason(w http.ResponseWriter, status int, err error, reason string) {
	writeErrorBody(w, status, err, &reason)
}

func writeErrorBody(w http.ResponseWriter, status int, err error, reason *string) {
	// What a client reads is never the chain's own text unless this
	// project wrote every part of it (wire.MessageOf): a driver error
	// names a table, a constraint and a SQLSTATE, and a decoder quotes
	// the body it could not read.
	body := machinegen.Error{Kind: errorx.KindOf(err).String(), Message: wire.MessageOf(err), Reason: reason}
	disclosed := wire.IsAuthored(err)
	if status >= http.StatusInternalServerError {
		// Above 500 nothing of the chain reaches the client, authored
		// or not: this project's own message can still carry a GRN,
		// never a row value.
		body.Message = "internal error"
		disclosed = false
	}
	// Whatever the body withheld reaches the request log instead, at
	// every status rather than above 500 alone: below it the kind's
	// own sentence replaced a chain naming the constraint and the
	// SQLSTATE a conflict was refused on.
	if recorder, ok := w.(causeRecorder); ok && !disclosed {
		recorder.RecordCause(err)
	}
	writeJSON(w, status, body)
}

// writeJSON encodes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	writeJSONAs(w, status, "application/json", v)
}

// writeJSONAs is writeJSON under a JSON media type of its own, such as
// a JWK Set's.
func writeJSONAs(w http.ResponseWriter, status int, mediaType string, v any) {
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(status)
	// A failed write after WriteHeader cannot be reported to the
	// caller; the server logs at the transport layer.
	_ = json.NewEncoder(w).Encode(v)
}

// tokenRoute reports whether r is one of the two routes that answer a
// token under machine-token.v1: an operator's exchange and an agent's
// mint (ADR-0100).
func tokenRoute(r *http.Request) bool {
	return r.URL.Path == "/tokens" ||
		(strings.HasPrefix(r.URL.Path, "/agents/") && strings.HasSuffix(r.URL.Path, "/tokens"))
}
