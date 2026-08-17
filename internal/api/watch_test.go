package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/watch"
)

// awaitReceiving blocks until the fixture's server is delivering what
// the relay announces. Announcing before it does would reach nobody for
// a reason that has nothing to do with who is watching.
func (f *fixture) awaitReceiving(t *testing.T) {
	t.Helper()
	select {
	case <-f.relay.ready:
	case <-time.After(time.Second):
		t.Fatal("the server is not delivering announced events")
	}
}

// forward announces one turn event as the machine listener does when an
// adapter forwards it — from another process, over the relay.
func (f *fixture) forward(t *testing.T, agent grn.GRN, kind, body string) {
	t.Helper()
	e, err := watch.NewEvent(kind, body)
	require.NoError(t, err)
	require.NoError(t, f.relay.Announce(context.Background(), agent, e))
}

// open holds a request open the way a browser watching an agent does,
// and returns the response with a reader over the stream. The returned
// function ends the request as leaving the page does.
func open(t *testing.T, base, path string, cookie *http.Cookie) (*http.Response, *bufio.Reader, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	require.NoError(t, err)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		require.NoError(t, err)
	}
	leave := func() {
		cancel()
		_ = resp.Body.Close()
	}
	t.Cleanup(leave)
	return resp, bufio.NewReader(resp.Body), leave
}

// nextFrame reads one server-sent frame: everything up to the blank line
// that ends it. A comment is a frame of its own, which is what a stream
// with nothing on it carries.
func nextFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	type read struct {
		frame string
		err   error
	}
	done := make(chan read, 1)
	go func() {
		var frame strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- read{err: err}
				return
			}
			if line == "\n" {
				done <- read{frame: frame.String()}
				return
			}
			frame.WriteString(line)
		}
	}()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		return got.frame
	case <-time.After(2 * time.Second):
		t.Fatal("nothing arrived on the stream")
		return ""
	}
}

// watchers sets up an org holding one agent, a member granted the watch
// and a member granted only the read, and returns the org, the agent and
// the two cookies.
func watchers(t *testing.T, f *fixture) (string, grn.GRN, *http.Cookie, *http.Cookie) {
	t.Helper()
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("watcher-sub"), org.ActionAgentWatch},
		permission{userGRNOf("watcher-sub"), org.ActionAgentRead},
		permission{userGRNOf("reader-sub"), org.ActionAgentRead},
	)
	watcher := f.sessionCookieFor(t, "watcher-sub", "Watcher")
	reader := f.sessionCookieFor(t, "reader-sub", "Reader")
	for _, subject := range []string{"watcher-sub", "reader-sub"} {
		require.NoError(t, f.orgs.AddMember(testCtx(t),
			org.NewMembership(orgGRNOf(slug), userGRNOf(subject), org.RoleMember)))
	}
	return slug, seedAgent(t, f, slug), watcher, reader
}

// TestWatchAgent_CarriesWhatIsForwardedWhileTheStreamIsOpen is the whole
// path a person watching sees: an event forwarded to the other process
// arrives here, and one forwarded before anyone was watching does not.
// The second is the control — a stream handed everything ever announced
// would read exactly like this one without it.
func TestWatchAgent_CarriesWhatIsForwardedWhileTheStreamIsOpen(t *testing.T) {
	f := newFixture(t)
	slug, held, watcher, _ := watchers(t, f)
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)
	f.awaitReceiving(t)

	// Nobody is watching yet. The event is announced, reaches no stream,
	// and is not kept for the one that opens next (ADR-0022).
	f.forward(t, held, "turn.start", "before anyone looked")
	require.Zero(t, f.leases.held(), "an agent nobody watches left a watch behind")

	path := "/orgs/" + slug + "/agents/" + held.Identifier + "/watch"
	resp, stream, leave := open(t, server.URL, path, watcher)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, 1, f.leases.held(), "the open stream is not counted")

	f.forward(t, held, "turn.output", "first line\nsecond line")
	// A body spanning lines is one data line per line, because a newline
	// written into one would end the event where the agent did not.
	assert.Equal(t, "event: turn.output\ndata: first line\ndata: second line\n", nextFrame(t, stream),
		"the first frame was not the event forwarded after the stream opened")

	// A stream with nothing on it still says so, which is what tells a
	// quiet agent from a connection that is gone.
	assert.Equal(t, ": keepalive\n", nextFrame(t, stream))

	// A second person watching the same agent gets their own stream, and
	// the first is undisturbed.
	secondResp, secondStream, _ := open(t, server.URL, path, watcher)
	require.Equal(t, http.StatusOK, secondResp.StatusCode)
	assert.Equal(t, 2, f.leases.held())
	f.forward(t, held, "turn.output", "both")
	assert.Contains(t, nextFrame(t, stream), "data: both")
	assert.Contains(t, nextFrame(t, secondStream), "data: both")

	// Leaving the page ends the watch, so nothing is counted for the
	// agent once its last watcher is gone.
	leave()
	assert.Eventually(t, func() bool { return f.leases.held() == 1 }, time.Second, 10*time.Millisecond,
		"a watch outlived the stream it belonged to")
}

// TestWatchAgent_RefusesACallerGrantedOnlyTheRead covers the grant
// ADR-0022 keeps separate. The same caller reading the same agent is the
// control: it is what stops being answered if the refusal comes from the
// session, the membership or the org rather than from the action.
func TestWatchAgent_RefusesACallerGrantedOnlyTheRead(t *testing.T) {
	f := newFixture(t)
	slug, held, watcher, reader := watchers(t, f)
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)

	path := "/orgs/" + slug + "/agents/" + held.Identifier + "/watch"
	refused, _, _ := open(t, server.URL, path, reader)
	assert.Equal(t, http.StatusForbidden, refused.StatusCode)
	assert.Zero(t, f.leases.held(), "a refused caller opened a watch")

	answered := f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", reader)
	require.Equal(t, http.StatusOK, answered.Code, "the read is refused too, so the refusal above proves nothing")

	granted, _, _ := open(t, server.URL, path, watcher)
	assert.Equal(t, http.StatusOK, granted.StatusCode)
}

// TestWatchAgent_AnswersNotFoundForAnAgentNobodyReported keeps a stream
// off a GRN no agent answers under: silence on this stream is what an
// agent that has said nothing looks like, so a typed identifier must not
// produce one. The seeded agent is the control.
func TestWatchAgent_AnswersNotFoundForAnAgentNobodyReported(t *testing.T) {
	f := newFixture(t)
	slug, held, watcher, _ := watchers(t, f)
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)

	missing, _, _ := open(t, server.URL, "/orgs/"+slug+"/agents/0000000000000000/watch", watcher)
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
	assert.Zero(t, f.leases.held(), "a watch was opened on an agent nobody reported")

	answered, _, _ := open(t, server.URL, "/orgs/"+slug+"/agents/"+held.Identifier+"/watch", watcher)
	assert.Equal(t, http.StatusOK, answered.StatusCode)
}
