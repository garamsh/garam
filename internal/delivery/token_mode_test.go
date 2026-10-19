package delivery_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
)

// agentTokenOf is an agent token as garam spells one, made of c.
func agentTokenOf(c string) string { return "garam_at_" + strings.Repeat(c, 64) }

// writeToken replaces the file at path with content in one rename, as
// an assignee does.
func writeToken(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".next"
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o600))
	require.NoError(t, os.Rename(tmp, path))
}

// tokenFile is a token file holding token.
func tokenFile(t *testing.T, token string) (*delivery.TokenFile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	writeToken(t, path, token)
	f, err := delivery.NewTokenFile(path)
	require.NoError(t, err)
	return f, path
}

// TestTokenFile_ReadsTheTokenTheFileHoldsAtEachRead holds the token
// mode to reading its file whole before every use and holding nothing:
// a replacement is the next read's, and a file that is missing, partly
// written or anything but one token is undecided. The whole token read
// before each is the control.
func TestTokenFile_ReadsTheTokenTheFileHoldsAtEachRead(t *testing.T) {
	first, second := agentTokenOf("a"), agentTokenOf("b")
	f, path := tokenFile(t, first+"\n")

	got, err := f.Read()
	require.NoError(t, err)
	assert.Equal(t, first, got, "the trailing newline a mounted Secret ends in is not the token's")
	writeToken(t, path, first+"\r\n")
	got, err = f.Read()
	require.NoError(t, err)
	assert.Equal(t, first, got, "nor a carriage return before it")
	writeToken(t, path, first+"\n")
	authorization, err := f.Authorization()
	require.NoError(t, err)
	assert.Equal(t, "Garam-Token "+first, authorization)
	fingerprint, err := f.Fingerprint()
	require.NoError(t, err)
	assert.Len(t, fingerprint, 64)
	assert.NotContains(t, fingerprint, strings.TrimPrefix(first, "garam_at_"), "the fingerprint is not the token")

	writeToken(t, path, second)
	got, err = f.Read()
	require.NoError(t, err)
	assert.Equal(t, second, got, "a replacement is read on the next read")
	replaced, err := f.Fingerprint()
	require.NoError(t, err)
	assert.NotEqual(t, fingerprint, replaced)

	unreadable := map[string]func(){
		"missing":            func() { require.NoError(t, os.Remove(path)) },
		"empty":              func() { writeToken(t, path, "") },
		"part way written":   func() { writeToken(t, path, first[:20]) },
		"another kind":       func() { writeToken(t, path, "garam_st_"+strings.Repeat("a", 64)) },
		"two tokens":         func() { writeToken(t, path, first+"\n"+second) },
		"a leading space":    func() { writeToken(t, path, " "+first) },
		"a trailing space":   func() { writeToken(t, path, first+" \n") },
		"past the read size": func() { writeToken(t, path, first+strings.Repeat(" ", 2048)) },
	}
	for name, spoil := range unreadable {
		t.Run(name, func(t *testing.T) {
			writeToken(t, path, first)
			_, err := f.Read()
			require.NoError(t, err, "the control: the whole token reads")
			spoil()
			_, err = f.Authorization()
			assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "undecided, never a refusal: %v", err)
			assert.NotContains(t, err.Error(), first, "the token is never in an error")
		})
	}
}

// recordingGaram is garam's listener recording the credential headers
// each call carried, and answering as answer says.
type recordingGaram struct {
	mu       sync.Mutex
	calls    []http.Header
	answer   int
	contract string
}

func (g *recordingGaram) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.calls = append(g.calls, r.Header.Clone())
	answer := g.answer
	g.mu.Unlock()
	if r.URL.Path == "/outbox-messages" {
		w.Header().Set("Garam-Contract-Version", g.contract)
		writeJSON(w, http.StatusCreated, machinegen.Message{Id: "stored-1"})
		return
	}
	if answer != 0 {
		writeJSON(w, answer, machinegen.Error{Kind: "unauthenticated", Message: "refused"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *recordingGaram) received() []http.Header {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]http.Header(nil), g.calls...)
}

// TestHTTPQueue_TokenModePresentsTheTokenFileOnEveryCall covers a
// token-mode adapter's calls to garam as ADR-0100 has them: the agent
// token in Authorization, read from its file for each call, and an
// outbox reply under durable-delivery.v2. A file that holds no whole
// token makes no call at all.
func TestHTTPQueue_TokenModePresentsTheTokenFileOnEveryCall(t *testing.T) {
	g := &recordingGaram{contract: delivery.TokenDeliveryContract}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(server.Close)
	f, path := tokenFile(t, agentTokenOf("a"))
	queue, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), delivery.HeldExecution("held-token"), f)
	require.NoError(t, err)

	_, err = queue.Claim(context.Background())
	require.ErrorIs(t, err, delivery.ErrNoMessageReady)
	writeToken(t, path, agentTokenOf("b"))
	_, err = queue.Claim(context.Background())
	require.ErrorIs(t, err, delivery.ErrNoMessageReady)
	_, err = queue.Post(context.Background(), delivery.Outbound{
		Recipient: senderGRN(), IdempotencyKey: "entry-1", Body: "the reply", CreatedAt: time.Now(),
	})
	require.NoError(t, err)

	calls := g.received()
	require.Len(t, calls, 3)
	assert.Equal(t, "Garam-Token "+agentTokenOf("a"), calls[0].Get("Authorization"))
	assert.Equal(t, "Garam-Token "+agentTokenOf("b"), calls[1].Get("Authorization"), "the rewritten file is the next call's")
	assert.Equal(t, "Garam-Token "+agentTokenOf("b"), calls[2].Get("Authorization"))
	assert.Equal(t, delivery.TokenDeliveryContract, calls[2].Get("Garam-Contract-Version"))
	assert.Equal(t, "held-token", calls[0].Get("Garam-Execution"), "beside the activation's token")

	writeToken(t, path, "garam_at_")
	_, err = queue.Claim(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "%v", err)
	assert.Len(t, g.received(), 3, "no call is made without a whole token")
}

// TestHTTPQueue_TokenModeReadsARefusedTokenAsUndecided holds a 401 to
// what it means in the token mode: the token expired or is not yet
// replaced, so the call is retried with the file read again and the
// execution is not closed. The certificate mode meeting the same answer
// is the control: there it is the fence.
func TestHTTPQueue_TokenModeReadsARefusedTokenAsUndecided(t *testing.T) {
	g := &recordingGaram{answer: http.StatusUnauthorized}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(server.Close)

	f, _ := tokenFile(t, agentTokenOf("a"))
	tokenExecution := delivery.HeldExecution("held-token")
	tokens, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), tokenExecution, f)
	require.NoError(t, err)
	_, err = tokens.Claim(context.Background())
	require.ErrorIs(t, err, delivery.ErrTokenRefused)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "undecided: %v", err)
	_, err = tokenExecution.Present()
	assert.NoError(t, err, "the execution stays open")

	certificateExecution := delivery.HeldExecution("held-token")
	certificates, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), certificateExecution, certificateMode())
	require.NoError(t, err)
	_, err = certificates.Claim(context.Background())
	require.ErrorIs(t, err, delivery.ErrFenced, "the control: a certificate refused is the fence")
	_, err = certificateExecution.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive)
}

// TestHTTPQueue_TokenModeReadsAForbiddenCallAsTheFence holds the token
// mode's undecided reading to a 401 alone: a 403 is garam refusing
// what the call presented beside a token it accepted, which is the
// fence, and closes the execution as in the certificate mode. A 401 on
// the same queue, which leaves the execution open, is the control.
func TestHTTPQueue_TokenModeReadsAForbiddenCallAsTheFence(t *testing.T) {
	f, _ := tokenFile(t, agentTokenOf("a"))
	for status, fenced := range map[int]bool{http.StatusForbidden: true, http.StatusUnauthorized: false} {
		g := &recordingGaram{answer: status}
		server := httptest.NewServer(http.HandlerFunc(g.serve))
		t.Cleanup(server.Close)
		execution := delivery.HeldExecution("held-token")
		queue, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), execution, f)
		require.NoError(t, err)
		_, err = queue.Claim(context.Background())
		_, presentErr := execution.Present()
		if fenced {
			require.ErrorIs(t, err, delivery.ErrFenced, "a 403 is the fence")
			assert.ErrorIs(t, presentErr, delivery.ErrInactive, "and closes the execution")
			continue
		}
		require.ErrorIs(t, err, delivery.ErrTokenRefused, "the control: a 401 is undecided")
		assert.NoError(t, presentErr)
	}
}

// TestHTTPAuthority_TokenModeCallsUnderAgentExecutionV2 covers the
// control path's token mode as ADR-0100 pins it: agent-execution.v2,
// the control token in Authorization read for each call, the placement
// token on activation alone in a header of its own, and a refused
// control token undecided. The certificate mode is the control on the
// last: there a 401 is a definite refusal.
func TestHTTPAuthority_TokenModeCallsUnderAgentExecutionV2(t *testing.T) {
	var mu sync.Mutex
	var calls []http.Header
	refuse := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Header.Clone())
		refusing := refuse
		mu.Unlock()
		w.Header().Set("Garam-Contract-Version", r.Header.Get("Garam-Contract-Version"))
		switch {
		case refusing:
			writeJSON(w, http.StatusUnauthorized, map[string]string{"kind": "unauthenticated", "message": "refused"})
		case strings.HasSuffix(r.URL.Path, "/activations"):
			writeJSON(w, http.StatusCreated, activationAnswer(binding()))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)
	f, path := tokenFile(t, agentTokenOf("c"))
	authority, err := delivery.NewHTTPAuthority(server.Client(), server.URL, agentGRN(), f)
	require.NoError(t, err)
	assert.Equal(t, delivery.TokenExecutionContract, authority.Contract())

	activation, err := authority.Activate(context.Background(), "request-1", binding(), "placement-1")
	require.NoError(t, err)
	writeToken(t, path, agentTokenOf("d"))
	status := &delivery.RuntimeStatus{
		Agent: agentGRN(), Epoch: "3", Generation: strings.Repeat("a", 32), Revision: "7",
		State: delivery.RuntimeServing, StartedAt: time.Now(),
	}
	require.NoError(t, authority.Report(context.Background(), activation.ID, status, time.Now()))

	require.Len(t, calls, 2)
	assert.Equal(t, "agent-execution.v2", calls[0].Get("Garam-Contract-Version"))
	assert.Equal(t, "Garam-Token "+agentTokenOf("c"), calls[0].Get("Authorization"))
	assert.Equal(t, "placement-1", calls[0].Get("Garam-Placement"))
	assert.Equal(t, "Garam-Token "+agentTokenOf("d"), calls[1].Get("Authorization"), "the rewritten file is the next call's")
	assert.Empty(t, calls[1].Get("Garam-Placement"), "a report carries no placement")

	mu.Lock()
	refuse = true
	mu.Unlock()
	_, err = authority.Activate(context.Background(), "request-1", binding(), "placement-1")
	require.ErrorIs(t, err, delivery.ErrTokenRefused)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "undecided: %v", err)

	certificates, err := delivery.NewHTTPAuthority(server.Client(), server.URL, agentGRN(), certificateMode())
	require.NoError(t, err)
	_, err = certificates.Activate(context.Background(), "request-1", binding(), "placement-1")
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), "the control: definite in the certificate mode: %v", err)
}

// tokenLoop is the loop of a token-mode adapter over the fakes: garam's
// token file, and control's beside it where c is not nil.
type tokenLoop struct {
	garam, control string
	placement      string
}

// startTokenMode starts a token-mode loop over the fakes and c, its
// garam token file holding garamToken at the start, and answers where
// its files are.
func startTokenMode(t *testing.T, queue *garamQueue, gateway *agentGateway, c *control, outbox *delivery.Outbox, garamToken string) tokenLoop {
	t.Helper()
	dir := t.TempDir()
	files := tokenLoop{garam: filepath.Join(dir, "garam-token")}
	writeToken(t, files.garam, garamToken)
	garam, err := delivery.NewTokenFile(files.garam)
	require.NoError(t, err)
	c.contract = delivery.TokenExecutionContract
	files.control = filepath.Join(dir, "control-token")
	writeToken(t, files.control, agentTokenOf("c"))
	files.placement = filepath.Join(dir, "placement-token")
	writeToken(t, files.placement, "placement-1")
	control, err := delivery.NewTokenFile(files.control)
	require.NoError(t, err)
	execution := delivery.NewFencedExecution()
	credentials := delivery.NewFileCredentials(files.placement, garam, control)
	if outbox == nil {
		outbox, err = delivery.NewOutbox(t.TempDir())
		require.NoError(t, err)
	}
	q, err := delivery.NewHTTPQueue(queue.server.Client(), queue.server.URL, agentGRN(), execution, garam)
	require.NoError(t, err)
	ingress, err := delivery.NewHTTPGateway(gateway.server.Client(), gateway.server.URL, agentGRN())
	require.NoError(t, err)
	start(t, delivery.Ends{
		Agent: agentGRN(), Queue: q, Gateway: ingress, Execution: execution,
		Credentials: credentials, Authority: c, Outbox: outbox,
	}, brief())
	return files
}

// carry waits for the first message to be posted and held, ends its
// turn, and waits for its completion: a claim, a renewal and a
// completion, each a protected call.
func carry(t *testing.T, queue *garamQueue, gateway *agentGateway) {
	t.Helper()
	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the message reaches the gateway")
	holding(t, queue, 0, 0)
	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the turn that named it completes it")
}

// TestRun_TokenModeRetriesARefusedTokenAndKeepsTheOutbox holds a
// refused garam token to ADR-0102's adapter rule: protected work waits
// and the outbox stays while the file is read again with backoff, and
// the same bytes are tried again rather than waited on for a change —
// garam accepting the unchanged token again is what resumes it here.
// A rewritten file is presented from the next call.
func TestRun_TokenModeRetriesARefusedTokenAndKeepsTheOutbox(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	queue.accept(agentTokenOf("z"))
	gateway := newAgentGateway(t)
	dir := t.TempDir()
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)
	writeEntry(t, dir, entryFor("entry-1", "the reply"))
	files := startTokenMode(t, queue, gateway, newControl(queue), outbox, agentTokenOf("a"))

	eventually(t, func() bool { return queue.claimsAttempted() >= 3 }, "a refused token is retried, not paused")
	retried := queue.claimsAttempted()
	time.Sleep(200 * time.Millisecond)
	assert.Less(t, queue.claimsAttempted()-retried, 40,
		"the retries back off: at most one per 20ms ceiling, where a loop without backoff spins")
	assert.Empty(t, queue.submissions(), "nothing is accepted on a refused token")
	assert.FileExists(t, filepath.Join(dir, "entry-1.json"), "the outbox is kept")

	queue.accept(agentTokenOf("a"))
	eventually(t, func() bool { return len(queue.submissions()) == 1 }, "the unchanged file is read again and accepted")

	queue.accept(agentTokenOf("b"))
	writeToken(t, files.garam, agentTokenOf("b"))
	refusedAt := len(queue.claimsPresenting())
	eventually(t, func() bool {
		presented := queue.claimsPresenting()
		return len(presented) > refusedAt+1 && presented[len(presented)-1] == "Garam-Token "+agentTokenOf("b")
	}, "the rewritten token is presented")
}

// TestRun_TokenModeWaitsOutATokenFileThatIsNotWhole holds a file that
// is missing or part way written to undecided: nothing reaches garam
// with it, and the loop goes on reading until the file is whole.
func TestRun_TokenModeWaitsOutATokenFileThatIsNotWhole(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "what the sender said")
	queue.accept(agentTokenOf("a"))
	gateway := newAgentGateway(t)
	files := startTokenMode(t, queue, gateway, newControl(queue), nil, "garam_at_aaaa")

	quietFor()
	assert.Zero(t, queue.claimsAttempted(), "nothing is presented from a file that is not whole")
	writeToken(t, files.garam, agentTokenOf("a"))
	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the control: the whole token carries the message")
}

// TestRun_FencedTokenModeKeepsItsTwoTokensApart is the fenced token
// mode end to end over the fakes: activation under agent-execution.v2's
// request identifier, and garam shown the garam token in Authorization
// with the activation's token in Garam-Execution, never the control
// token.
func TestRun_FencedTokenModeKeepsItsTwoTokensApart(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "what the sender said")
	queue.accept(agentTokenOf("a"))
	gateway := newAgentGateway(t)
	c := newControl(queue)
	startTokenMode(t, queue, gateway, c, nil, agentTokenOf("a"))

	carry(t, queue, gateway)
	require.NotEmpty(t, c.requests())
	assert.Equal(t, delivery.RequestID(delivery.TokenExecutionContract, binding(), "placement-1"), c.requests()[0])
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for i, presented := range queue.presented {
		assert.Equal(t, "Garam-Token "+agentTokenOf("a"), presented, "garam is shown the garam token alone")
		assert.Equal(t, "token-activation-1-1", queue.executions[i])
	}
}

// TestRun_ARewrittenTokenReasksAPausedActivation is the token mode's
// re-arming: a definite refusal pauses, no timer asks again, and a
// rewritten token file asks once more. The authority's answer decides:
// where it activates protected work resumes, and where it refuses the
// pause holds and nothing more is asked. Against garam, the re-ask of
// the same generation after an agent:recover is the refusal, and the
// next generation resumes (adapter.md §Token mode,
// TestAdapterTokenMode_ARecoveryIsResumedByTheNextGeneration).
func TestRun_ARewrittenTokenReasksAPausedActivation(t *testing.T) {
	for name, after := range map[string]struct {
		answer  error
		resumes bool
	}{
		"an authority that answers activates": {answer: nil, resumes: true},
		"a standing fence is refused again": {
			answer: errorx.New(errorx.KindFailedPrecondition, "control refused 409 epoch_superseded"), resumes: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			queue := newGaramQueue(t, time.Second, "what the sender said")
			queue.accept(agentTokenOf("a"), agentTokenOf("b"))
			gateway := newAgentGateway(t)
			c := newControl(queue)
			c.answer(errorx.New(errorx.KindFailedPrecondition, "control refused 409 epoch_superseded"))
			files := startTokenMode(t, queue, gateway, c, nil, agentTokenOf("a"))

			eventually(t, func() bool { return len(c.requests()) == 1 }, "the activation is asked and refused")
			quietFor()
			require.Len(t, c.requests(), 1, "the control: no timer asks again")

			c.answer(after.answer)
			writeToken(t, files.garam, agentTokenOf("b"))
			eventually(t, func() bool { return len(c.requests()) == 2 }, "the rewritten token asks again")
			asked := c.requests()
			assert.Equal(t, asked[0], asked[1], "a token is not intent: the same request")
			if after.resumes {
				eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the activation resumes protected work")
				return
			}
			quietFor()
			assert.Len(t, c.requests(), 2, "the pause holds, and nothing more is asked")
			assert.Zero(t, queue.claimsAttempted(), "no protected call is made")
		})
	}
}
