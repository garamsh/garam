package machineapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	connectiongen "github.com/garamsh/garam/platform/api/cluster-connection/v1alpha1"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/wire"
)

// ClusterConnection serves cluster-connection v1alpha1, the protocol an
// instance of garam-bot-connector speaks (ADR-0184, ADR-0186,
// ADR-0192), from the interface's own specification, vendored at
// api/cluster-connection/v1alpha1/. Registration and rotation are the
// app system's own, served to an instance of any app; declarations and
// reports only to an instance whose app lists them among its machine
// operations. Its refusals carry that interface's error body (T-5)
// rather than this listener's.
type ClusterConnection struct {
	instances app.Service
	bots      bot.Service
	tokenURL  string
}

var _ connectiongen.ServerInterface = (*ClusterConnection)(nil)

// NewClusterConnection serves instances and what is declared for them,
// answering tokenURL as where a registered instance exchanges its
// assertions (REG-6).
func NewClusterConnection(instances app.Service, bots bot.Service, tokenURL string) *ClusterConnection {
	return &ClusterConnection{instances: instances, bots: bots, tokenURL: tokenURL}
}

// Register spends the registration token and binds the connector's key
// (REG-1 to REG-8). It reads no identity: a registration carries none.
func (c *ClusterConnection) Register(w http.ResponseWriter, r *http.Request) {
	// The jwk is read as the connector sent it, here and in RotateKey:
	// the generated PublicJwk keeps only the members it declares, so it
	// would drop the private members REG-2 has the server refuse.
	var body struct {
		RegistrationToken string          `json:"registrationToken"`
		Jwk               json.RawMessage `json:"jwk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeConnectionError(w, errorx.New(errorx.KindInvalidArgument, "the body is not a registration request"))
		return
	}
	g, err := c.instances.Register(r.Context(), sourceOf(r), body.RegistrationToken, body.Jwk)
	if err != nil {
		writeConnectionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, connectiongen.Registration{
		Cluster: g.String(), ClientId: g.String(), TokenUrl: c.tokenURL,
	})
}

// RotateKey adds the successor key the caller's current key authorized
// and the successor proved (ROT-1 to ROT-4).
func (c *ClusterConnection) RotateKey(w http.ResponseWriter, r *http.Request) {
	caller, ok := connectedInstance(w, r, false)
	if !ok {
		return
	}
	var body struct {
		Jwk           json.RawMessage `json:"jwk"`
		Authorization string          `json:"authorization"`
		Proof         string          `json:"proof"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeConnectionError(w, errorx.New(errorx.KindInvalidArgument, "the body is not a key rotation request"))
		return
	}
	rotation, err := c.instances.Rotate(r.Context(), caller, body.Jwk, body.Authorization, body.Proof)
	if err != nil {
		writeConnectionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, connectiongen.KeyRotation{
		Kid: rotation.KID, PredecessorRetiresAt: rotation.PredecessorRetiresAt,
	})
}

// ReportPath is the report route's path, whose body is bounded at
// bot.ReportMaxBytes rather than the listener's own bound.
const ReportPath = "/cluster-connection/v1alpha1/clusters/self/reports"

// errReportTooLarge answers a report past bot.ReportMaxBytes
// (REP-6).
var errReportTooLarge = errorx.New(errorx.KindInvalidArgument, "the report is over 4 MiB")

// declarationPage and change are DeclarationPage and Change as the wire
// carries them. The generated Change decodes its object into maps,
// which would round spec's numbers through float64; the object is sent
// as garam normalized it.
type declarationPage struct {
	Cursor  string   `json:"cursor"`
	Full    bool     `json:"full,omitempty"`
	More    bool     `json:"more,omitempty"`
	Changes []change `json:"changes"`
}

type change struct {
	Op         string          `json:"op"`
	ID         string          `json:"id"`
	Generation int64           `json:"generation"`
	Object     json.RawMessage `json:"object,omitempty"`
}

// WatchDeclarations long-polls the declarations placed on the calling
// cluster (DECL-1 to DECL-6).
func (c *ClusterConnection) WatchDeclarations(w http.ResponseWriter, r *http.Request, params connectiongen.WatchDeclarationsParams) {
	caller, ok := connectedInstance(w, r, true)
	if !ok {
		return
	}
	wait := bot.MaxWait
	if params.Wait != nil {
		wait = time.Duration(*params.Wait) * time.Second
	}
	page, err := c.bots.WatchDeclarations(r.Context(), caller, params.After, wait)
	if errors.Is(err, context.Canceled) {
		// The connector went; there is no one to answer.
		return
	}
	if err != nil {
		writeConnectionError(w, err)
		return
	}
	out := declarationPage{Cursor: page.Cursor, Full: page.Full, More: page.More,
		Changes: make([]change, 0, len(page.Changes))}
	for _, ch := range page.Changes {
		out.Changes = append(out.Changes, change{Op: ch.Op, ID: ch.ID, Generation: ch.Generation, Object: ch.Object})
	}
	writeJSON(w, http.StatusOK, out)
}

// Report keeps the calling cluster's report where it is its latest
// (REP-1 to REP-9), answering 204 whether it was kept or was older.
func (c *ClusterConnection) Report(w http.ResponseWriter, r *http.Request) {
	caller, ok := connectedInstance(w, r, true)
	if !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bot.ReportMaxBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeConnectionError(w, errReportTooLarge)
		return
	}
	if err != nil {
		writeConnectionError(w, errorx.Wrap(err, "read report", errorx.KindInvalidArgument))
		return
	}
	report, err := bot.ParseReport(raw)
	if err != nil {
		writeConnectionError(w, err)
		return
	}
	if err := c.bots.Report(r.Context(), caller, report); err != nil {
		writeConnectionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// connectedInstance is the instance the request's token stands for, and
// writes the refusal where it stands for none: a token that proved
// nothing, one of another kind of caller, or, on a route that is its
// app's machine operation rather than the app system's own, an
// instance whose app does not list the route, is answered 401
// (AUTH-2).
func connectedInstance(w http.ResponseWriter, r *http.Request, operation bool) (grn.GRN, bool) {
	identity, ok := machine.IdentityOf(r)
	if !ok {
		err := machine.RefusalOf(r)
		if err == nil {
			err = errorx.New(errorx.KindUnauthenticated, "the request carries no instance's token")
		}
		writeConnectionError(w, err)
		return grn.GRN{}, false
	}
	if !app.IsInstanceGRN(identity.GRN()) {
		writeConnectionError(w, errorx.New(errorx.KindUnauthenticated, "the token is not an instance's"))
		return grn.GRN{}, false
	}
	if operation && !identity.Reach().Allows(r.Pattern) {
		writeConnectionError(w, errorx.New(errorx.KindUnauthenticated, "the instance's app does not reach this route"))
		return grn.GRN{}, false
	}
	return identity.GRN(), true
}

// ConnectionBindingError answers a request the generated router
// refused while binding its parameters, in the interface's error body.
func ConnectionBindingError(w http.ResponseWriter, _ *http.Request, _ error) {
	writeConnectionError(w, errorx.New(errorx.KindInvalidArgument, "a request parameter could not be read"))
}

// connectionCodes names each sentinel whose code the interface lists
// (T-5); any other refusal takes its status's code, connectionCode's.
// A status of 0 keeps the one the refusal's kind maps to.
var connectionCodes = []struct {
	err    error
	code   string
	status int
}{
	{app.ErrRegistrationRefused, "registration_refused", 0},
	{app.ErrInvalidProof, "invalid_proof", 0},
	{app.ErrTooManyKeys, "too_many_keys", 0},
	// No kind maps to 410, which DECL-6 answers, or to 413, which REP-6
	// answers.
	{bot.ErrCursorExpired, "cursor_expired", http.StatusGone},
	{errReportTooLarge, "report_too_large", http.StatusRequestEntityTooLarge},
}

// connectionStatus is the status a refusal is answered at.
func connectionStatus(err error) int {
	for _, c := range connectionCodes {
		if c.status != 0 && errors.Is(err, c.err) {
			return c.status
		}
	}
	return wire.StatusOf(errorx.KindOf(err))
}

// connectionCode is the interface's code for a refusal at status.
func connectionCode(err error, status int) string {
	for _, c := range connectionCodes {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	switch {
	case status == http.StatusUnauthorized:
		return "unauthorized"
	case status == http.StatusNotFound:
		return "not_found"
	case status == http.StatusTooManyRequests:
		return "throttled"
	case status >= http.StatusInternalServerError:
		return "internal"
	}
	return "invalid_request"
}

// writeConnectionError renders err as the interface's error body
// (T-5), at the status this listener maps its kind to, with the
// message withheld as writeErrorBody withholds it.
func writeConnectionError(w http.ResponseWriter, err error) {
	status := connectionStatus(err)
	var exceeded *operator.Exceeded
	if errors.As(err, &exceeded) {
		w.Header().Set("Retry-After", strconv.Itoa(exceeded.RetryAfterSeconds()))
	}
	message, disclosed := wire.MessageOf(err), wire.IsAuthored(err)
	if status >= http.StatusInternalServerError {
		message, disclosed = "internal error", false
	}
	if recorder, ok := w.(causeRecorder); ok && !disclosed {
		recorder.RecordCause(err)
	}
	writeJSON(w, status, connectiongen.Error{Error: connectionCode(err, status), Message: message})
}
