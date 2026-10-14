package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/wire"
)

// RecoverPanics answers a handler that panicked with a 500, which
// net/http does not do on its own (stack-go.md §4). It answers and does
// not log: the stack travels in the cause, and reaches the request line
// through the recorder a handled 5xx already reports on.
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
// text/plain and repeats what the caller submitted — the inner error of
// an unparseable number is a strconv error, and one quotes the value it
// could not parse.
//
// A missing contract version is answered as an unsupported one, so the
// refusal names the version the route serves.
//
// The composition root supplies this as ErrorHandlerFunc, and nothing
// else answers on this path.
func BindingError(w http.ResponseWriter, _ *http.Request, err error) {
	var header *apigen.RequiredHeaderError
	if errors.As(err, &header) && header.ParamName == operator.ContractVersionHeader {
		contract(w, "")
		return
	}
	writeError(w, errorx.New(errorx.KindInvalidArgument, bindingMessage(err)))
}

// bindingMessage names what the router refused, built from the
// parameter name the generated error carries and the kind of failure it
// is — never from the error's own text, which is where the submitted
// value would travel. A parameter name is the specification's word and
// not the caller's.
func bindingMessage(err error) string {
	var (
		required  *apigen.RequiredParamError
		header    *apigen.RequiredHeaderError
		format    *apigen.InvalidParamFormatError
		tooMany   *apigen.TooManyValuesForParamError
		cookie    *apigen.UnescapedCookieParamError
		unmarshal *apigen.UnmarshalingParamError
	)
	switch {
	case errors.As(err, &required):
		return "query parameter " + required.ParamName + " is required"
	case errors.As(err, &header):
		return "header parameter " + header.ParamName + " is required"
	case errors.As(err, &format):
		return "parameter " + format.ParamName + " is not in the required format"
	case errors.As(err, &tooMany):
		return "parameter " + tooMany.ParamName + " was given more than once"
	case errors.As(err, &cookie):
		return "cookie parameter " + cookie.ParamName + " is not correctly escaped"
	case errors.As(err, &unmarshal):
		return "parameter " + unmarshal.ParamName + " is not valid JSON"
	default:
		return "a request parameter could not be read"
	}
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

// writeError renders an errorx error as the spec's Error body. The
// HTTP status is the wire mapping of the error kind (internal/wire);
// errorx stays transport-agnostic and the mapping is shared by every
// surface.
func writeError(w http.ResponseWriter, err error) {
	status := wire.StatusOf(errorx.KindOf(err))
	var exceeded *operator.Exceeded
	if errors.As(err, &exceeded) {
		w.Header().Set("Retry-After", strconv.Itoa(exceeded.RetryAfterSeconds()))
	}
	// What a client reads is never the chain's own text unless this
	// project wrote every part of it (wire.MessageOf): a driver error
	// names a table, a constraint and a SQLSTATE, and a decoder quotes
	// the body it could not read.
	body := apigen.Error{Kind: errorx.KindOf(err).String(), Message: wire.MessageOf(err)}
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
