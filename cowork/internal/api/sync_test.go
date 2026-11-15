package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/api"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
)

// asked is the platform: it allows every check and sends each batch it
// is asked, in order.
type asked chan []platform.Check

func (a asked) Identify(context.Context, platform.Credential) (platform.Caller, error) {
	return platform.Caller{}, nil
}

func (a asked) Organizations(context.Context, platform.Caller) ([]platform.Organization, error) {
	return nil, nil
}

func (a asked) Allowed(ctx context.Context, _ platform.Caller, _ string, checks []platform.Check) ([]bool, error) {
	select {
	case a <- checks:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make([]bool, len(checks))
	for i := range out {
		out[i] = true
	}
	return out, nil
}

// readablePages finds every page.
type readablePages struct{ page.Service }

func (readablePages) Get(_ context.Context, _ platform.Caller, _, id string) (page.Page, error) {
	return page.Page{ID: id}, nil
}

// idleRoom holds a connection open until its client leaves.
type idleRoom struct{ collab.Service }

func (idleRoom) Serve(ctx context.Context, _ collab.Room, conn collab.Conn) error {
	for {
		if _, err := conn.Read(ctx); err != nil {
			return err
		}
	}
}

// openSync opens a page's sync connection and answers what the
// platform is asked, batch by batch.
func openSync(t *testing.T) asked {
	t.Helper()
	platformAsked := make(asked, 16)
	openSyncOn(t, platformAsked)
	return platformAsked
}

// openSyncOn opens a page's sync connection against p and answers it.
func openSyncOn(t *testing.T, p platform.Platform) *websocket.Conn {
	t.Helper()
	s := api.NewServer(readablePages{}, idleRoom{}, nil, nil, nil, p, nil, 5*time.Millisecond)
	mux := http.NewServeMux()
	mux.HandleFunc(api.SyncPath, s.SyncPage)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/orgs/acme/pages/p1/sync", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func next(t *testing.T, a asked) []platform.Check {
	t.Helper()
	select {
	case checks := <-a:
		return checks
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the platform was not asked")
		return nil
	}
}

func TestSyncPage_StatesItsWriteQuestionAsAProbe(t *testing.T) {
	checks := next(t, openSync(t))

	require.Len(t, checks, 1, "the upgrade asks one question")
	require.Equal(t, platform.ActionPageWrite, checks[0].Action, "it is the write question")
	assert.Equal(t, "p1", checks[0].ID)
	assert.True(t, checks[0].Probe, "it decides whether the connection is writable")
}

func TestSyncPage_RecheckStatesNothingOfItsReadAndAProbeOfItsWrite(t *testing.T) {
	a := openSync(t)
	next(t, a) // the upgrade's own question
	checks := next(t, a)

	require.Len(t, checks, 2, "the re-check asks the read, then the write")
	require.Equal(t, platform.ActionPageRead, checks[0].Action)
	require.Equal(t, platform.ActionPageWrite, checks[1].Action)
	assert.False(t, checks[0].Probe, "the read is a request the person made")
	assert.True(t, checks[1].Probe, "the write decides whether the connection is writable")
}

// signedOut is the platform for a session that ends: it refuses the
// credential once out is set, and allows every check throughout.
type signedOut struct {
	asked
	out atomic.Bool
}

func (p *signedOut) Identify(ctx context.Context, cred platform.Credential) (platform.Caller, error) {
	if p.out.Load() {
		return platform.Caller{}, errorx.New(errorx.KindUnauthenticated, "signed out")
	}
	return p.asked.Identify(ctx, cred)
}

// closeSignedOut is the close code a client reads as signed out.
const closeSignedOut = websocket.StatusCode(4401)

func TestSyncPage_RecheckClosesASessionThePlatformNoLongerAccepts(t *testing.T) {
	p := &signedOut{asked: make(asked, 16)}
	conn := openSyncOn(t, p)
	closed := make(chan websocket.StatusCode, 1)
	go func() {
		_, _, err := conn.Read(context.Background())
		closed <- websocket.CloseStatus(err)
	}()
	next(t, p.asked) // the upgrade's own question
	next(t, p.asked) // a re-check that passed
	select {
	case code := <-closed:
		require.FailNow(t, "the control: while the session stands the connection is open", "closed with %d", code)
	default:
	}

	p.out.Store(true)

	select {
	case code := <-closed:
		assert.Equal(t, closeSignedOut, code)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the connection stayed open for a session the platform refuses")
	}
}
