package machine_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/machine"
)

// sightings is the Recorder seam in memory: every sighting written, and
// a failure to answer with instead where one is set.
type sightings struct {
	mu      sync.Mutex
	written []machine.Sighting
	fail    error
}

func (s *sightings) Record(_ context.Context, sighting machine.Sighting) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.written = append(s.written, sighting)
	return nil
}

func (s *sightings) all() []machine.Sighting {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]machine.Sighting(nil), s.written...)
}

func (s *sightings) failWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

// clock is a time a test moves by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestEvidence_WritesACallersPathOncePerWindow is ADR-0109's bound on
// what evidence costs the request path. The second sighting of one
// caller and path inside the window writes nothing, and the controls
// are what does write: another path for the same caller, another
// caller on the same path, and the first caller's path again once the
// window has passed.
func TestEvidence_WritesACallersPathOncePerWindow(t *testing.T) {
	recorder := &sightings{}
	at := &clock{now: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	evidence := machine.NewEvidence(recorder, time.Minute, at.read)
	one, other := operatorGRNOf("acme", "one"), operatorGRNOf("acme", "other")
	ctx := context.Background()

	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathPrivateKeyJWT})
	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathPrivateKeyJWT})
	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathToken})
	evidence.Note(ctx, machine.Sighting{Caller: other, Path: machine.PathPrivateKeyJWT})
	at.advance(59 * time.Second)
	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathPrivateKeyJWT})
	at.advance(time.Second)
	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathPrivateKeyJWT})

	assert.Equal(t, []machine.Sighting{
		{Caller: one, Path: machine.PathPrivateKeyJWT},
		{Caller: one, Path: machine.PathToken},
		{Caller: other, Path: machine.PathPrivateKeyJWT},
		{Caller: one, Path: machine.PathPrivateKeyJWT},
	}, recorder.all())
}

// TestEvidence_AFailedWriteIsTriedAgainOnlyOnceTheWindowPasses covers
// a database that cannot be written: the note returns rather than
// failing the request, and it is not retried on every request after.
// The write after the window is the control that the caller was not
// dropped for good.
func TestEvidence_AFailedWriteIsTriedAgainOnlyOnceTheWindowPasses(t *testing.T) {
	recorder := &sightings{}
	recorder.failWith(errorx.New(errorx.KindUnavailable, "database down"))
	at := &clock{now: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	evidence := machine.NewEvidence(recorder, time.Minute, at.read)
	one := agentGRN("acme", "bot-1")
	ctx := context.Background()

	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathToken})
	recorder.failWith(nil)
	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathToken})
	require.Empty(t, recorder.all(), "the failed write is not retried inside the window")

	at.advance(time.Minute)
	evidence.Note(ctx, machine.Sighting{Caller: one, Path: machine.PathToken})
	assert.Equal(t, []machine.Sighting{{Caller: one, Path: machine.PathToken}}, recorder.all())
}

// TestEvidence_NilRecordsNothing is the listener served without
// evidence, as a test that is not about it serves it.
func TestEvidence_NilRecordsNothing(t *testing.T) {
	var evidence *machine.Evidence
	assert.NotPanics(t, func() {
		evidence.Note(context.Background(), machine.Sighting{Caller: agentGRN("acme", "bot-1"), Path: machine.PathToken})
	})
}

// TestReportIssuance_ReachesOnlyTheRequestAwaitingIt holds the cell an
// issuing handler reports into. A request nothing awaits keeps nothing,
// and the awaited one is the control that a report is read at all.
func TestReportIssuance_ReachesOnlyTheRequestAwaitingIt(t *testing.T) {
	issued := machine.Issuance{Subject: operatorGRNOf("acme", "one"), Audience: true,
		Until: time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)}

	unawaited := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	assert.NotPanics(t, func() { machine.ReportIssuance(unawaited, issued) })

	awaited, read := machine.AwaitIssuance(httptest.NewRequest(http.MethodPost, "/tokens", nil))
	_, ok := read()
	require.False(t, ok, "nothing is read before the handler reports")
	machine.ReportIssuance(awaited, issued)
	got, ok := read()
	require.True(t, ok)
	assert.Equal(t, issued, got)
}

// TestNewServer_NotesHowEachRequestAuthenticated holds that a request
// a token authenticated is noted on the token path under the identity
// it carries, and that a request that authenticated nothing — no
// credential, or a token that is not live — is noted nowhere.
func TestNewServer_NotesHowEachRequestAuthenticated(t *testing.T) {
	known := newTokens()
	operator := operatorGRNOf("acme", "one")
	known.set("garam_ot_live", operator)
	recorder := &sightings{}
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveEvidence(t, dir, known, machine.NewEvidence(recorder, time.Minute, nil), methodHandler())

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}}
	t.Cleanup(client.CloseIdleConnections)
	status, body, _ := presenting(t, client, url, "garam_ot_live")
	require.Equal(t, http.StatusOK, status, body)
	status, _, _ = presenting(t, client, url, "garam_ot_withdrawn")
	require.Equal(t, http.StatusUnauthorized, status)
	status, _, _ = presenting(t, client, url, "")
	require.Equal(t, http.StatusUnauthorized, status)

	assert.ElementsMatch(t, []machine.Sighting{
		{Caller: operator, Path: machine.PathToken},
	}, recorder.all())
}
