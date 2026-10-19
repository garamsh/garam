package machine

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Path is how one authentication by a machine caller was decided
// (ADR-0109).
type Path string

const (
	// PathToken is a request the listener authenticated by a token.
	PathToken Path = "token"
	// PathSharedSecret is an exchange of an operator's or a
	// supervisor's shared-secret credential for a token.
	PathSharedSecret Path = "exchange_shared_secret"
	// PathPrivateKeyJWT is an operator's exchange of a signed client
	// assertion for a token (ADR-0102).
	PathPrivateKeyJWT Path = "exchange_private_key_jwt"
	// PathAgentMint is an agent token its assignee minted.
	PathAgentMint Path = "agent_mint"
	// PathAudienceToken is an exchange or a mint whose token names an
	// audience, the hosted operator it is shown to.
	PathAudienceToken Path = "audience_token"
)

// Sighting is one authentication [Evidence] records: who, by which
// path, and, for an exchange or a mint, until when the token it issued
// is accepted. It carries nothing the caller presented.
type Sighting struct {
	Caller grn.GRN
	Path   Path
	// IssuedUntil is the issued token's expiry, and zero for a request
	// the listener authenticated, which issued nothing.
	IssuedUntil time.Time
}

// recordTimeout bounds one write of evidence, which a request waits on.
const recordTimeout = time.Second

// Evidence records the sightings the listener and the issuing routes
// hand it, at most once per caller and path per window in this process
// (ADR-0109). It is best effort: a write that fails is logged and the
// request goes on, and a nil Evidence records nothing.
//
// The write is on the request's path, so the window is what bounds
// its cost: one upsert per caller and path per window, whatever the
// caller's request rate.
type Evidence struct {
	recorder Recorder
	window   time.Duration
	now      func() time.Time

	mu       sync.Mutex
	recorded map[evidenceKey]time.Time
	swept    time.Time
}

type evidenceKey struct {
	caller string
	path   Path
}

// NewEvidence records through recorder, coalescing within window. A nil
// now takes time.Now.
func NewEvidence(recorder Recorder, window time.Duration, now func() time.Time) *Evidence {
	if now == nil {
		now = time.Now
	}
	return &Evidence{recorder: recorder, window: window, now: now, recorded: make(map[evidenceKey]time.Time)}
}

// Note records s unless the same caller and path were recorded within
// the window. A failed write is logged at warn and not retried until
// the window passes, so a database that is down costs each caller one
// attempt per window rather than one per request.
func (e *Evidence) Note(ctx context.Context, s Sighting) {
	if e == nil || !e.claim(s) {
		return
	}
	// A caller that disconnects has still authenticated.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if err := e.recorder.Record(ctx, s); err != nil {
		slog.WarnContext(ctx, "authentication evidence not recorded",
			slog.String("path", string(s.Path)), slog.String("error", err.Error()))
	}
}

// claim reports whether s is due a write, and marks it written. Entries
// older than the window are swept once per window, so the map holds at
// most the callers seen within the last two.
func (e *Evidence) claim(s Sighting) bool {
	now := e.now()
	key := evidenceKey{caller: s.Caller.String(), path: s.Path}
	e.mu.Lock()
	defer e.mu.Unlock()
	if now.Sub(e.swept) >= e.window {
		for k, at := range e.recorded {
			if now.Sub(at) >= e.window {
				delete(e.recorded, k)
			}
		}
		e.swept = now
	}
	if at, ok := e.recorded[key]; ok && now.Sub(at) < e.window {
		return false
	}
	e.recorded[key] = now
	return true
}

// Issuance is what an exchange or a mint issued: the subject its
// handler resolved, whether the token names an audience, and until when
// it is accepted. The handler reports it with [ReportIssuance], and the
// middleware that classified the route records it (ADR-0109).
type Issuance struct {
	Subject  grn.GRN
	Audience bool
	Until    time.Time
}

type issuanceKey struct{}

type issuanceCell struct {
	issued *Issuance
}

// AwaitIssuance returns r carrying a cell an issuing handler reports
// into, and the read of what it reported once the handler returns.
func AwaitIssuance(r *http.Request) (*http.Request, func() (Issuance, bool)) {
	cell := &issuanceCell{}
	read := func() (Issuance, bool) {
		if cell.issued == nil {
			return Issuance{}, false
		}
		return *cell.issued, true
	}
	return r.WithContext(context.WithValue(r.Context(), issuanceKey{}, cell)), read
}

// ReportIssuance hands what a handler issued to the middleware awaiting
// it. A request nothing awaits keeps nothing.
func ReportIssuance(r *http.Request, i Issuance) {
	if cell, ok := r.Context().Value(issuanceKey{}).(*issuanceCell); ok {
		cell.issued = &i
	}
}
