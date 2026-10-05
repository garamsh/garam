package delivery_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
)

// control is the lifecycle authority as agent-execution.v1 describes
// it: the same request identifier answers the same activation with a
// newer token, which garam then admits alone, and a new identifier is a
// new activation. refuse answers every activation instead.
type control struct {
	queue *garamQueue

	mu          sync.Mutex
	activations map[string]delivery.Activation
	asked       []string
	bindings    []delivery.Binding
	placements  []string
	refuse      error
	reports     []controlReport
}

// controlReport is one status report control took.
type controlReport struct {
	activation string
	status     delivery.RuntimeStatus
}

func newControl(queue *garamQueue) *control {
	return &control{queue: queue, activations: map[string]delivery.Activation{}}
}

func (c *control) Activate(_ context.Context, requestID string, b delivery.Binding, placementToken string) (*delivery.Activation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, requestID)
	c.bindings = append(c.bindings, b)
	c.placements = append(c.placements, placementToken)
	if c.refuse != nil {
		return nil, c.refuse
	}
	a, replay := c.activations[requestID]
	if !replay {
		a = delivery.Activation{ID: fmt.Sprintf("activation-%d", len(c.activations)+1), Binding: b}
	}
	a.TokenVersion++
	a.Token = delivery.Token(fmt.Sprintf("token-%s-%d", a.ID, a.TokenVersion))
	c.activations[requestID] = a
	c.queue.admit(string(a.Token))
	return &a, nil
}

func (c *control) Report(_ context.Context, activation string, s *delivery.RuntimeStatus, _ time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reports = append(c.reports, controlReport{activation: activation, status: *s})
	return nil
}

// answer makes every later activation answered with err, nil restoring
// the ordinary answer.
func (c *control) answer(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refuse = err
}

func (c *control) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.asked...)
}

func (c *control) reported() []controlReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]controlReport(nil), c.reports...)
}

// tokenOf is the Authorization a claim carries under the n-th token of
// the n-th activation control made.
func tokenOf(activation, version int) string {
	return fmt.Sprintf("Garam-Execution token-activation-%d-%d", activation, version)
}

// runFenced starts a fenced loop against the fakes, forwarding from a
// fresh outbox when none is named.
func runFenced(t *testing.T, queue *garamQueue, gateway *agentGateway, c *control, m *mounted, outbox *delivery.Outbox) context.CancelFunc {
	t.Helper()
	if outbox == nil {
		var err error
		outbox, err = delivery.NewOutbox(t.TempDir())
		require.NoError(t, err)
	}
	return start(t, ends(t, queue, gateway, delivery.NewFencedExecution(), c, m, outbox), brief())
}

// quietFor waits long enough for the loop to have looked many times,
// for a test asserting that something did not happen. Twenty status
// intervals is what "no timer resends it" is measured over.
func quietFor() { time.Sleep(100 * time.Millisecond) }

// TestRun_ClaimsNothingUntilTheGenerationIsActivated holds the fenced
// loop to collaborating only after activation: while control is
// undecided the same request is retried and nothing is claimed, and
// once it answers every claim carries the activation's token. The
// claims after the answer are the control: the loop does claim, only
// not before.
func TestRun_ClaimsNothingUntilTheGenerationIsActivated(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "what the sender said")
	gateway := newAgentGateway(t)
	c := newControl(queue)
	c.answer(errorx.New(errorx.KindUnavailable, "control answered 503"))
	runFenced(t, queue, gateway, c, newMounted(), nil)

	eventually(t, func() bool { return len(c.requests()) >= 3 }, "an undecided activation is retried")
	asked := c.requests()
	for _, id := range asked {
		assert.Equal(t, asked[0], id, "a retry is the same request")
	}
	assert.Zero(t, queue.claimsAttempted(), "nothing is claimed before an activation is held")

	c.answer(nil)
	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the control: claims flow once it is held")
	for _, presented := range queue.claimsPresenting() {
		assert.Equal(t, tokenOf(1, 1), presented, "every claim carries the activation's token")
	}
	binding := c.bindings[0]
	assert.Equal(t, delivery.Binding{
		Agent: agentGRN(), Epoch: "3", Generation: strings.Repeat("a", 32), Revision: "7",
	}, binding, "the binding asked for is what the runtime reported")
	assert.Equal(t, "placement-1", c.placements[0], "under the mounted placement token")
}

// TestRun_ReplaysItsActivationAfterARestart is a restarted adapter
// beside the same runtime: it asks for the same activation under the
// same request identifier, receives a newer token, and presents only
// that one. garam admitting the newest token alone is what fences the
// first process.
func TestRun_ReplaysItsActivationAfterARestart(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "what the sender said")
	gateway := newAgentGateway(t)
	c := newControl(queue)
	m := newMounted()
	stop := runFenced(t, queue, gateway, c, m, nil)
	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the first process carries the message")
	stop()

	runFenced(t, queue, gateway, c, m, nil)
	eventually(t, func() bool { return queue.deliveries(0) == 2 && len(gateway.posted()) == 2 },
		"the second process is handed the message again")

	asked := c.requests()
	require.Len(t, asked, 2)
	assert.Equal(t, asked[0], asked[1], "the restart replays the same request")
	presented := queue.claimsPresenting()
	assert.Equal(t, tokenOf(1, 2), presented[len(presented)-1], "and presents the newer token of the same activation")
	assert.Contains(t, presented, tokenOf(1, 1), "the control: the first process presented the first token")
	assert.Equal(t, 1, gateway.sourceCount(), "the runtime holds one source")
}

// TestRun_ActivatesTheGenerationTheRuntimeNowReports is a runtime that
// restarted beside a running adapter: a new generation is new intent,
// asked under a new request identifier, and the old token is not
// presented again, and so is a new placement. Until then an unchanged
// status asks nothing, which is the control.
func TestRun_ActivatesTheGenerationTheRuntimeNowReports(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	gateway := newAgentGateway(t)
	c := newControl(queue)
	m := newMounted()
	runFenced(t, queue, gateway, c, m, nil)

	eventually(t, func() bool { return queue.claimsAttempted() > 0 }, "the first generation is activated")
	quietFor()
	require.Len(t, c.requests(), 1, "the control: an unchanged runtime asks for nothing more")

	gateway.report("generation", strings.Repeat("b", 32))
	eventually(t, func() bool { return len(c.requests()) == 2 }, "the new generation is asked for")
	asked := c.requests()
	assert.NotEqual(t, asked[0], asked[1], "under a new request")
	assert.Equal(t, strings.Repeat("b", 32), c.bindings[1].Generation)
	eventually(t, func() bool {
		presented := queue.claimsPresenting()
		return presented[len(presented)-1] == tokenOf(2, 1)
	}, "claims carry the new generation's token")

	m.replace("placement-2", "1001")
	eventually(t, func() bool { return len(c.requests()) == 3 }, "another placement is asked for while the gate is open")
	asked = c.requests()
	assert.NotEqual(t, asked[1], asked[2], "under a new request")
	assert.Equal(t, "placement-2", c.placements[2])
}

// TestRun_StopsProtectedWorkOnAFenceUntilTheLeafChanges is garam
// refusing the token the adapter holds: claims stop, a reply written
// meanwhile stays in the outbox and is not offered, and nothing is
// resent on a timer. A renewed certificate re-arms it, and the replay
// it makes is the same request. The claims before the refusal are the
// control.
func TestRun_StopsProtectedWorkOnAFenceUntilTheLeafChanges(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	gateway := newAgentGateway(t)
	c := newControl(queue)
	m := newMounted()
	dir := t.TempDir()
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)
	runFenced(t, queue, gateway, c, m, outbox)

	eventually(t, func() bool { return queue.claimsAttempted() > 1 }, "the control: claims flow under the activation")
	queue.admit("a token another activation issued")
	eventually(t, func() bool { return queue.claimsFenced() == 1 }, "garam refuses the token held")
	refusedAt := queue.claimsAttempted()
	writeEntry(t, dir, entryFor("entry-1", "the reply"))
	quietFor()
	assert.Equal(t, refusedAt, queue.claimsAttempted(), "no claim after the fence")
	assert.Empty(t, queue.submissions(), "no reply is offered while fenced")
	assert.FileExists(t, dir+"/entry-1.json", "the outbox is kept")
	assert.Len(t, c.requests(), 1, "nothing is asked again on a timer")

	m.replace("placement-1", "1002")
	eventually(t, func() bool { return len(queue.submissions()) == 1 }, "a renewed certificate resumes protected work")
	asked := c.requests()
	require.Len(t, asked, 2)
	assert.Equal(t, asked[0], asked[1], "renewal keeps the request identifier")
	assert.Equal(t, "Garam-Execution token-activation-1-2", queue.submissions()[0].Authorization)
	eventually(t, func() bool { return queue.claimsAttempted() > refusedAt }, "and claims resume")
}

// claimsFenced is how many claims garam's fence refused.
func (q *garamQueue) claimsFenced() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.fenced
}

// TestRun_PausesADefiniteActivationRefusalUntilAnInputChanges holds a
// definite refusal to what re-arms it: no timer resends it, a changed
// placement token is a new request, and a renewed certificate replays
// the same one. Each re-arming is the control on the pause before it.
func TestRun_PausesADefiniteActivationRefusalUntilAnInputChanges(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "what the sender said")
	gateway := newAgentGateway(t)
	c := newControl(queue)
	c.answer(errorx.New(errorx.KindFailedPrecondition, "control refused 403 placement_not_current"))
	m := newMounted()
	runFenced(t, queue, gateway, c, m, nil)

	eventually(t, func() bool { return len(c.requests()) == 1 }, "the activation is asked for")
	quietFor()
	assert.Len(t, c.requests(), 1, "a definite refusal is not resent on a timer")

	m.replace("placement-2", "1001")
	eventually(t, func() bool { return len(c.requests()) == 2 }, "a changed placement token re-arms it")
	quietFor()
	asked := c.requests()
	require.Len(t, asked, 2, "and pauses again on the refusal")
	assert.NotEqual(t, asked[0], asked[1], "another placement is another request")

	c.answer(nil)
	m.replace("placement-2", "1002")
	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "a renewed certificate re-arms it too")
	asked = c.requests()
	assert.Equal(t, asked[1], asked[2], "under the same request: the certificate is not part of it")
	assert.NotContains(t, queue.claimsPresenting(), "", "nothing was claimed without a token")
}

// TestRun_ReportsRuntimeStatusAfterActivationAndOnChange holds the
// report to its two moments: after the activation it is reported under,
// and when what the runtime says changes. An unchanged runtime is the
// control: it is not reported again.
func TestRun_ReportsRuntimeStatusAfterActivationAndOnChange(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	gateway := newAgentGateway(t)
	c := newControl(queue)
	c.answer(errorx.New(errorx.KindUnavailable, "control answered 503"))
	runFenced(t, queue, gateway, c, newMounted(), nil)

	eventually(t, func() bool { return len(c.requests()) >= 2 }, "the activation is retried")
	assert.Empty(t, c.reported(), "nothing is reported before an activation is held")
	c.answer(nil)
	eventually(t, func() bool { return len(c.reported()) == 1 }, "the status is reported once activated")
	first := c.reported()[0]
	assert.Equal(t, "activation-1", first.activation)
	assert.Equal(t, delivery.RuntimeServing, first.status.State)
	assert.Equal(t, "7", first.status.Revision)
	quietFor()
	assert.Len(t, c.reported(), 1, "the control: an unchanged runtime is not reported again")

	gateway.report("state", "draining")
	eventually(t, func() bool { return len(c.reported()) == 2 }, "a change is reported")
	assert.Equal(t, delivery.RuntimeDraining, c.reported()[1].status.State)
}

// TestRun_LegacyResumesAFencedAgentOnlyOnARenewedCertificate is the
// legacy mode meeting garam's fence, which it cannot clear by
// activating: it stops, waits on no timer, and tries again only once
// the certificate changes.
func TestRun_LegacyResumesAFencedAgentOnlyOnARenewedCertificate(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	gateway := newAgentGateway(t)
	m := newMounted()
	e := legacyEnds(t, queue, gateway, nil)
	e.Credentials = m
	start(t, e, brief())

	eventually(t, func() bool { return queue.claimsAttempted() > 1 }, "the control: a legacy agent claims")
	queue.admit("a token the agent was never given")
	eventually(t, func() bool { return queue.claimsFenced() == 1 }, "garam requires a token the legacy adapter does not hold")
	refusedAt := queue.claimsAttempted()
	quietFor()
	assert.Equal(t, refusedAt, queue.claimsAttempted(), "nothing is claimed again on a timer")

	m.replace("", "1002")
	eventually(t, func() bool { return queue.claimsAttempted() > refusedAt }, "a renewed certificate tries again")
}
