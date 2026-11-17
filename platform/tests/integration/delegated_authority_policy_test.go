//go:build policyservice

package integration_test

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
	decisiongrpc "github.com/garamsh/garam/platform/internal/policydecision/grpc"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	feedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	feedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
	"github.com/garamsh/garam/platform/internal/signer"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// The cases here hold the platform's delegated operation authority and
// controller proof against the stack's real policy service (ADR-0146
// items 14 and 15): what the service was fed decides, read current, and
// what the platform decides itself it decides before it asks. They are
// the policyservice suite's, which needs the stack's policy service.

// askedOf wraps the real policy service and records every question the
// platform put to it.
type askedOf struct {
	policydecision.Service
	mu      sync.Mutex
	asked   []policydecision.Question
	batches [][]policydecision.Question
}

func (a *askedOf) Evaluations(ctx context.Context, qs []policydecision.Question) ([]policydecision.Decision, error) {
	a.mu.Lock()
	a.batches = append(a.batches, append([]policydecision.Question(nil), qs...))
	a.mu.Unlock()
	return a.Service.Evaluations(ctx, qs)
}

// batchesAsked are the Evaluations calls the platform made, each as
// the questions it carried.
func (a *askedOf) batchesAsked() [][]policydecision.Question {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([][]policydecision.Question(nil), a.batches...)
}

func (a *askedOf) Evaluate(ctx context.Context, q policydecision.Question) (policydecision.Decision, error) {
	a.mu.Lock()
	a.asked = append(a.asked, q)
	a.mu.Unlock()
	return a.Service.Evaluate(ctx, q)
}

func (a *askedOf) questions() []policydecision.Question {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]policydecision.Question(nil), a.asked...)
}

// realPolicy is the stack's policy service, the feed that delivers the
// platform's state to it, and what the platform asked of it.
type realPolicy struct {
	asked  *askedOf
	client policyv1.PolicyServiceClient
	pool   *pgxpool.Pool
	store  *feedpg.Store
	sender *feedgrpc.Sender
}

// startRealPolicy turns the feed on, so what the test writes next is
// marked, and dials the service as `serve api` does.
func startRealPolicy(t *testing.T, p *pgxpool.Pool) *realPolicy {
	t.Helper()
	addr, caFile, keyFile, issuer := serviceEnv(t)
	startFeed(t, p)
	key, err := signer.LoadKey(keyFile)
	require.NoError(t, err)
	conn, err := feedgrpc.Dial(addr, caFile, issuer, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := policyv1.NewPolicyServiceClient(conn)
	return &realPolicy{
		asked: &askedOf{Service: decisiongrpc.New(client)}, client: client, pool: p,
		store: feedpg.NewStore(p), sender: feedgrpc.NewSender(client),
	}
}

// The suite reads the stack through the variables
// `make -C platform policy-service` sets.
const (
	serviceAddrEnv   = "POLICY_SERVICE_ADDR"
	serviceCAEnv     = "POLICY_SERVICE_CA"
	serviceKeyEnv    = "POLICY_SERVICE_KEY"
	serviceIssuerEnv = "POLICY_SERVICE_ISSUER"
)

// serviceEnv names the stack's policy service, the CA that signed its
// certificate and the key and issuer the platform's tokens carry.
func serviceEnv(t *testing.T) (addr, caFile, keyFile, issuer string) {
	t.Helper()
	get := func(name string) string {
		v := os.Getenv(name)
		require.NotEmpty(t, v, "%s names the stack's policy service; run make -C platform policy-service", name)
		return v
	}
	return get(serviceAddrEnv), get(serviceCAEnv), get(serviceKeyEnv), get(serviceIssuerEnv)
}

// deliverAll sends the feed until the outbox is empty, and fails if
// the service refuses any of it, since the state would not have
// arrived.
func deliverAll(t *testing.T, p *pgxpool.Pool, store *feedpg.Store, sender policyfeed.Sender) {
	t.Helper()
	for range 50 {
		result, err := store.Deliver(context.Background(), policyfeed.BatchLimit, sender)
		require.NoError(t, err)
		require.Empty(t, result.Refused, "the service refused an entity")
		require.Empty(t, result.SetAside, "the service refused a role link")
		require.Empty(t, result.ScopedSetAside, "the service refused a scoped role link")
		if len(outboxMarks(t, p)) == 0 {
			return
		}
	}
	t.Fatal("the outbox did not empty")
}

// deliver sends the outbox to the service until it is empty.
func (r *realPolicy) deliver(t *testing.T) {
	t.Helper()
	deliverAll(t, r.pool, r.store, r.sender)
}

// deliverWhileRunning sends the outbox to the service until the test
// ends, which is what the route that writes a grant waits on.
func (r *realPolicy) deliverWhileRunning(t *testing.T) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			_, _ = r.store.Deliver(ctx, 100, r.sender)
			time.Sleep(50 * time.Millisecond)
		}
	}()
	t.Cleanup(func() { stop(); <-done })
}

// TestDelegatedAuthorityPolicy_TheMemberHalfIsTheServicesDecision asks
// the service for a member's half of an authority as the platform does,
// and shows an allowance and a denial of it by the role the member
// holds, an owner being the control for the operation a member does
// not hold; and that a grant the platform has ended is refused before
// the service is asked.
func TestDelegatedAuthorityPolicy_TheMemberHalfIsTheServicesDecision(t *testing.T) {
	p := pool(t)
	rp := startRealPolicy(t, p)
	s := newAuthorityStackDeciding(t, p, rp.asked)
	rp.deliver(t)
	rp.deliverWhileRunning(t)
	d := s.grant(t, "g-1", nil, "iam:agent.create", "iam:agent.configure")
	agentGRN := s.provisionAgent(t)
	require.Eventually(t, func() bool { return len(outboxMarks(t, p)) == 0 }, 10*time.Second, 50*time.Millisecond,
		"the service holds the organization, its members, the grant and the agent")

	before := len(rp.asked.questions())
	status, body := s.mint(t, s.member, "iam:agent.create", s.controller.String(), "req-1")
	assert.Equal(t, http.StatusCreated, status, "a member's role carries agent.create: %s", body)
	status, body = s.mint(t, s.member, "iam:agent.configure", agentGRN, "req-2")
	assert.Equal(t, http.StatusForbidden, status, "a member's role does not carry agent.configure: %s", body)
	status, body = s.mint(t, s.owner, "iam:agent.configure", agentGRN, "req-2")
	assert.Equal(t, http.StatusCreated, status, "the owner's does: %s", body)

	asked := rp.asked.questions()[before:]
	require.Len(t, asked, 3, "one question for each mint")
	for _, q := range asked {
		assert.True(t, q.Current, "asked read current: %+v", q)
	}

	status, body = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d.GrantId+`"}`)
	require.Equal(t, http.StatusNoContent, status, body)
	ended := len(rp.asked.questions())
	status, body = s.mint(t, s.owner, "iam:agent.create", s.controller.String(), "req-3")
	assert.Equal(t, http.StatusForbidden, status, "the grant ended: %s", body)
	assert.Len(t, rp.asked.questions(), ended, "the platform refused an ended grant before it asked")
}

// TestDelegatedAuthorityPolicy_TheOperatorHalfIsTheServicesDecision
// proves a controller to its hosted operator against the service: the
// operator holding the fed operation is allowed, one that does not is
// denied by the service, a grant the platform has ended is refused
// before the service is asked, and a guardrail of the organization
// forbidding the operation denies the operator that holds it. The
// proof before each refusal is its control.
func TestDelegatedAuthorityPolicy_TheOperatorHalfIsTheServicesDecision(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	rp := startRealPolicy(t, p)
	s := newProofStackDeciding(t, p, rp.asked)
	owner := grn.New(grn.RootOwner, user.ServiceType, "ada")
	require.NoError(t, userpg.New(p).Create(ctx, user.New(owner, "ada")))
	require.NoError(t, orgpg.New(p).AddMember(ctx, org.NewMembership(s.acme, owner, org.RoleOwner)))
	rp.deliver(t)
	writer := &policyv1.Subject{Type: "Iam::User", Id: owner.String()}
	policy := "grn:" + s.acme.Identifier + "/iam/policy/no-activation"
	// The service outlives the test: a guardrail left in it would deny
	// the next run's control, so none is left before or after.
	dropGuardrail := func() {
		_, _ = rp.client.DeletePolicy(ctx, &policyv1.DeletePolicyRequest{Writer: writer, Org: s.acme.String(), Policy: policy})
	}
	dropGuardrail()
	t.Cleanup(dropGuardrail)

	got := s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	require.Equal(t, http.StatusOK, got.status, "the hosted operator holds agent.activate: %s", got.body)
	asked := rp.asked.questions()
	require.Len(t, asked, 1)
	assert.Equal(t, policydecision.Question{
		Subject: s.hosted, Action: string(operator.OperationAgentActivate), Resource: s.acme, Current: true,
	}, asked[0], "the actor, the action and the object, read current, and nothing else")

	holding, err := rp.asked.Evaluate(ctx, policydecision.Question{
		Subject: s.hosted, Action: string(operator.OperationAgentActivate), Resource: s.acme, Current: true})
	require.NoError(t, err)
	assert.True(t, holding.Allowed, "an operator whose fed operations hold the action")
	for _, lacking := range []grn.GRN{s.controller, s.other} {
		d, err := rp.asked.Evaluate(ctx, policydecision.Question{
			Subject: lacking, Action: string(operator.OperationAgentActivate), Resource: s.acme, Current: true})
		require.NoError(t, err)
		assert.False(t, d.Allowed, "%s is fed no operations", lacking)
	}

	require.NoError(t, s.grants.EndGrant(ctx, s.acme, s.hosted, s.grant, time.Now()))
	before := len(rp.asked.questions())
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	assert.Equal(t, http.StatusForbidden, got.status, "the grant ended: %s", got.body)
	assert.Len(t, rp.asked.questions(), before, "the platform refused an ended grant before it asked")

	started := s.clock.now()
	second := s.delegate(t, "g-2", nil, started.Add(30*time.Minute), s.controller)
	rp.deliver(t)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	require.Equal(t, http.StatusOK, got.status, "granted again: %s", got.body)

	// A grant past its expiry is still on record, so it is the platform
	// that refuses it, before the service is asked.
	s.clock.set(started.Add(30*time.Minute + time.Second))
	before = len(rp.asked.questions())
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	assert.Equal(t, http.StatusForbidden, got.status, "the grant expired: %s", got.body)
	assert.Len(t, rp.asked.questions(), before, "the platform refused an expired grant before it asked")

	s.clock.set(started)
	s.delegate(t, "g-3", &second, started.Add(time.Hour), s.controller)
	rp.deliver(t)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	require.Equal(t, http.StatusOK, got.status, "granted again, unexpired: %s", got.body)

	_, err = rp.client.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{
		Writer: writer, Org: s.acme.String(), Policy: policy, Name: "no-activation",
		Body: &policyv1.PolicyBody{Body: &policyv1.PolicyBody_Guardrail{
			Guardrail: `forbid (principal, action == Iam::Action::"agent.activate", resource);`}},
	})
	require.NoError(t, err)
	_, err = rp.client.ActivatePolicy(ctx, &policyv1.ActivatePolicyRequest{Writer: writer, Org: s.acme.String(), Policy: policy})
	require.NoError(t, err)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	assert.Equal(t, http.StatusForbidden, got.status, "a guardrail of the organization forbids it: %s", got.body)
}
