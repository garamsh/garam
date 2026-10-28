package integration_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// The authority boundaries are substituted here; the stores behind
// the service, the forwarded tokens' included, are the real ones.

type grantingAuthorizer struct {
	err   error
	calls atomic.Int32
}

func (a *grantingAuthorizer) Authorize(context.Context, grn.GRN, grn.GRN, grn.GRN, agent.LifecycleOperation) (agent.Grant, error) {
	a.calls.Add(1)
	if a.err != nil {
		return agent.Grant{}, a.err
	}
	return agent.Grant{ID: "grant-1", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type boundOperations struct {
	refs, handoffs map[string]*agent.OperationBinding
}

func (o *boundOperations) ResolveReference(_ context.Context, _ grn.GRN, ref string) (*agent.OperationBinding, error) {
	if b, ok := o.refs[ref]; ok {
		return b, nil
	}
	return nil, errorx.New(errorx.KindPermissionDenied, "reference no longer authorized")
}

func (o *boundOperations) ResolveHandoff(_ context.Context, _ grn.GRN, authority string) (*agent.OperationBinding, error) {
	if b, ok := o.handoffs[authority]; ok {
		return b, nil
	}
	return nil, errorx.New(errorx.KindNotFound, "handoff unknown")
}

type lifecycle struct {
	fence
	authorizer *grantingAuthorizer
	operations *boundOperations
	service    agent.ExecutionService
	audience   grn.GRN
	// shown holds the token forwarded for each label, minted to be
	// shown to audience the first time the label is asked for.
	shown map[string]string
}

func newLifecycle(t *testing.T) lifecycle {
	t.Helper()
	f := newFence(t)
	l := lifecycle{
		fence:      f,
		authorizer: &grantingAuthorizer{},
		operations: &boundOperations{refs: map[string]*agent.OperationBinding{}, handoffs: map[string]*agent.OperationBinding{}},
		audience:   registerOperator(t, f.p, scopedGRN("acme", agent.ServiceTypeOperator, "control")),
		shown:      map[string]string{},
	}
	l.service = agent.NewExecutionService(f.agents, f.exec, l.authorizer, l.operations, f.agents)
	return l
}

// forwarded is the token forwarded for label: minted for the agent to
// be shown to the audience, in the lineage the execution holds when the
// label is first asked for. "expired" is one past its expiry, and
// "forged" one no mint produced.
func (l lifecycle) forwarded(t *testing.T, label string) agent.ForwardedToken {
	t.Helper()
	if label == "forged" {
		return agent.ForwardedToken(agent.AccessTokenPrefix + strings.Repeat("0", 64))
	}
	if value, ok := l.shown[label]; ok {
		return agent.ForwardedToken(value)
	}
	token := l.mintedFor(t, &l.audience)
	if label == "expired" {
		_, err := l.p.Exec(context.Background(), `UPDATE agent_tokens SET expires_at = now() - interval '1 second',
			issued_at = now() - interval '1 hour' WHERE token_hash = $1`, token.Hash())
		require.NoError(t, err)
	}
	l.shown[label] = token.Value
	return agent.ForwardedToken(token.Value)
}

// credentialOfLabel is what the fence is handed for label's token.
func (l lifecycle) credentialOfLabel(t *testing.T, label string) agent.Credential {
	t.Helper()
	holder, err := l.agents.AccessTokenShownTo(context.Background(), agent.HashAccessToken(string(l.forwarded(t, label))), l.audience)
	require.NoError(t, err)
	return agent.Credential{Serial: holder.Serial, NotBefore: holder.IssuedAt, NotAfter: holder.ExpiresAt}
}

func (l lifecycle) input(t *testing.T, requestID string, gen agent.Generation, replaces *agent.ActivationID, label string) agent.ActivationInput {
	t.Helper()
	return agent.ActivationInput{
		Agent: l.agent, RequestID: agent.RequestID(requestID), Epoch: l.epochNow(),
		Generation: gen, Replaces: replaces, Credential: l.forwarded(t, label),
	}
}

func (l lifecycle) epochNow() int {
	a, err := l.agents.GetAssignment(context.Background(), l.agent)
	if err != nil {
		return 0
	}
	return a.Epoch
}

// configureRef binds a reference minted to configure this agent,
// through this audience, at the assignment it is on now.
func (l lifecycle) configureRef(ref string) *agent.OperationBinding {
	b := &agent.OperationBinding{
		Ref: ref, Kind: agent.OperationKindConfigure, Org: orgRootGRN("acme"), Audience: l.audience,
		Target: l.agent, Assignee: l.controller, Epoch: l.epochNow(),
	}
	l.operations.refs[ref] = b
	return b
}

func TestExecutionService_OnlyALifecycleAuthorityActivates(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)

	_, err := l.service.Activate(ctx, l.agent, l.input(t, "r1", generation(1), nil, "leaf-1"))
	assert.ErrorIs(t, err, agent.ErrNotALifecycleAuthority, "the agent's own credential cannot activate")

	l.authorizer.err = errorx.New(errorx.KindPermissionDenied, "no grant")
	_, err = l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	l.authorizer.err = errorx.New(errorx.KindUnavailable, "policy engine unreachable")
	_, err = l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "unknown freshness refuses")
	e, err := l.exec.GetExecution(ctx, l.agent)
	require.NoError(t, err)
	assert.Nil(t, e.Current, "nothing was written by a refusal")

	l.authorizer.err = nil
	res, err := l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err, "control: the authorized audience activates")
	assert.True(t, res.Created)
	assert.Equal(t, agent.AdmissionAdmitted, l.admit(t, &res.Token, l.credentialOfLabel(t, "leaf-1")))

	_, err = l.service.Activate(ctx, l.audience, l.input(t, "r2", generation(2), &res.Activation.ID, "forged"))
	assert.ErrorIs(t, err, agent.ErrTokenNotProven, "a token no mint produced")
}

func TestExecutionService_APendingRevisionNeedsAReferenceForThisAgentAndAssignment(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	first, err := l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)
	latest := &first.Activation.ID

	ref := func(s string) *string { return &s }
	attempt := func(name string, binding func(*agent.OperationBinding), want error) {
		t.Run(name, func(t *testing.T) {
			b := l.configureRef(name)
			if binding != nil {
				binding(b)
			}
			in := l.input(t, "req-"+name, generation(len(name)+10), latest, "leaf-1")
			in.OperationRef = ref(name)
			res, err := l.service.Activate(ctx, l.audience, in)
			if want != nil {
				assert.ErrorIs(t, err, want)
				return
			}
			require.NoError(t, err)
			latest = &res.Activation.ID
		})
	}
	attempt("configure-this-agent", nil, nil)
	attempt("another-agent", func(b *agent.OperationBinding) { b.Target = scopedGRN("acme", agent.ServiceType, "other") }, agent.ErrReferenceMismatch)
	attempt("another-audience", func(b *agent.OperationBinding) { b.Audience = scopedGRN("acme", agent.ServiceTypeOperator, "x") }, agent.ErrReferenceMismatch)
	attempt("stale-assignment", func(b *agent.OperationBinding) { b.Epoch-- }, agent.ErrReferenceMismatch)
	attempt("another-org", func(b *agent.OperationBinding) { b.Org = orgRootGRN("globex") }, agent.ErrReferenceMismatch)
	attempt("creation", func(b *agent.OperationBinding) { b.Kind = agent.OperationKindCreate }, agent.ErrReferenceMismatch)

	t.Run("a reference whose actor lost authority", func(t *testing.T) {
		in := l.input(t, "revoked", generation(50), latest, "leaf-1")
		in.OperationRef = ref("unknown-or-revoked")
		_, err := l.service.Activate(ctx, l.audience, in)
		assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	})
	t.Run("control: a restart of the applied revision needs no reference", func(t *testing.T) {
		_, err := l.service.Activate(ctx, l.audience, l.input(t, "restart", generation(51), latest, "leaf-1"))
		assert.NoError(t, err)
	})
}

func TestExecutionService_IntrospectionAnswersForTheForwardedToken(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	g := func(n int) *agent.Generation { v := generation(n); return &v }

	in, err := l.service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-1"), nil)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialCurrent, in.Credential)
	assert.Equal(t, agent.GenerationNone, in.Generation, "before any activation")
	assert.Equal(t, l.controller, in.Assignee)

	expired, err := l.service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "expired"), nil)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialFenced, expired.Credential, "an expired token is fenced, not malformed")

	res, err := l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		supplied *agent.Generation
		want     agent.GenerationStanding
	}{
		{"the active generation", g(1), agent.GenerationCurrent},
		{"another generation", g(2), agent.GenerationNotCurrent},
		{"none supplied while one is active", nil, agent.GenerationNotSupplied},
	} {
		in, err := l.service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-1"), tc.supplied)
		require.NoError(t, err)
		assert.Equal(t, tc.want, in.Generation, tc.name)
	}

	require.NoError(t, l.service.Deactivate(ctx, l.audience, l.agent, res.Activation.ID))
	after, err := l.service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-1"), g(1))
	require.NoError(t, err)
	assert.Equal(t, agent.GenerationNotCurrent, after.Generation, "a deactivated generation")
	none, err := l.service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-1"), nil)
	require.NoError(t, err)
	assert.Equal(t, agent.GenerationNone, none.Generation)

	_, err = l.service.Introspect(ctx, l.agent, l.agent, l.forwarded(t, "leaf-1"), nil)
	assert.ErrorIs(t, err, agent.ErrNotALifecycleAuthority)
}

func (l lifecycle) handoff(authority, body string) {
	l.operations.handoffs[authority] = &agent.OperationBinding{
		Ref: "ref-" + authority, Kind: agent.OperationKindRecover, Org: orgRootGRN("acme"), Audience: l.audience,
		Target: l.agent, Assignee: l.controller, Epoch: l.epochNow(),
		BodySHA256: sha256.Sum256([]byte(body)), ExpiresAt: time.Now().Add(5 * time.Minute),
	}
}

// TestExecutionService_AnOperatorForwardsNoTokenMintedForGaram holds
// the forwarded token's second form to a supervisor (ADR-0100): an
// operator that minted its agent's token for garam, forwarding it on
// introspection, proves nothing. A token the same operator minted to
// be shown to the caller, forwarded by that caller, is the control.
func TestExecutionService_AnOperatorForwardsNoTokenMintedForGaram(t *testing.T) {
	ctx := context.Background()
	f, _ := newLineageFence(t)
	service := agent.NewExecutionService(f.agents, f.exec, &grantingAuthorizer{}, nil, f.agents)

	own := f.mintedIn(t)
	_, err := service.Introspect(ctx, f.controller, f.agent, agent.ForwardedToken(own.Value), nil)
	require.ErrorIs(t, err, agent.ErrTokenNotProven, "an operator forwards no token minted for garam")

	hosted := registerOperator(t, f.p, scopedGRN("acme", agent.ServiceTypeOperator, "hosted"))
	shown := f.mintedFor(t, &hosted)
	in, err := service.Introspect(ctx, hosted, f.agent, agent.ForwardedToken(shown.Value), nil)
	require.NoError(t, err, "control: a token shown to the caller proves the agent")
	assert.Equal(t, agent.CredentialCurrent, in.Credential)
}

// countingShown counts the forwarded-token reads it passes on.
type countingShown struct {
	agent.ShownTokenReader
	reads int
}

func (c *countingShown) AccessTokenShownTo(ctx context.Context, tokenHash string, audience grn.GRN) (agent.AccessTokenHolder, error) {
	c.reads++
	return c.ShownTokenReader.AccessTokenShownTo(ctx, tokenHash, audience)
}

// holds that a forwarded value no mint could have produced is refused
// before the store is asked (ADR-0100). The well-formed unknown token
// beside it is asked and refused the same way, so what spared the store
// is the shape.
func TestExecutionService_AMalformedForwardedTokenReadsNothing(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	reader := &countingShown{ShownTokenReader: l.agents}
	service := agent.NewExecutionService(l.agents, l.exec, l.authorizer, l.operations, reader)

	for _, malformed := range []string{
		"", agent.AccessTokenPrefix + strings.Repeat("0", 63), agent.AccessTokenPrefix + strings.Repeat("Z", 64),
		"garam_ot_" + strings.Repeat("0", 64),
	} {
		_, err := service.Introspect(ctx, l.audience, l.agent, agent.ForwardedToken(malformed), nil)
		require.ErrorIs(t, err, agent.ErrTokenNotProven, malformed)
	}
	assert.Zero(t, reader.reads, "a malformed token reads nothing")

	_, err := service.Introspect(ctx, l.audience, l.agent, agent.ForwardedToken(agent.AccessTokenPrefix+strings.Repeat("0", 64)), nil)
	require.ErrorIs(t, err, agent.ErrTokenNotProven)
	assert.Equal(t, 1, reader.reads, "control: a well-formed unknown token is read")
}

func TestExecutionService_RecoveryIsBoundToItsExactBodyAndAnsweredOnce(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	res, err := l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)
	before := l.credentialOfLabel(t, "leaf-1")

	body := `{"requestId":"rec-1","epoch":"1"}`
	l.handoff("h1", body)
	in := agent.RecoveryInput{
		Agent: l.agent, RequestID: "rec-1", Epoch: l.epochNow(), Authority: "h1", Body: []byte(body + " "),
	}
	_, err = l.service.Recover(ctx, l.audience, in)
	assert.ErrorIs(t, err, agent.ErrBodyMismatch)

	in.Body = []byte(body)
	recovered, err := l.service.Recover(ctx, l.audience, in)
	require.NoError(t, err, "control: the exact body recovers, the handoff was not spent")
	assert.Equal(t, agent.AdmissionTokenStale, l.admit(t, &res.Token, before))

	again, err := l.service.Recover(ctx, l.audience, in)
	require.NoError(t, err)
	assert.Equal(t, recovered, again, "a lost response is answered with the recorded lineage")

	changed := `{"requestId":"rec-1","epoch":"2"}`
	l.handoff("h2", changed)
	replay := in
	replay.Authority, replay.Body = "h2", []byte(changed)
	_, err = l.service.Recover(ctx, l.audience, replay)
	assert.ErrorIs(t, err, agent.ErrRecoveryConflict, "a completed request identifier with another binding is refused, not answered")

	l.authorizer.err = errorx.New(errorx.KindPermissionDenied, "authority withdrawn")
	_, err = l.service.Recover(ctx, l.audience, in)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "a retry is authorized afresh before it is answered")
	l.authorizer.err = nil

	l.handoff("configure", body)
	l.operations.handoffs["configure"].Kind = agent.OperationKindConfigure
	in.Authority, in.RequestID = "configure", "rec-2"
	_, err = l.service.Recover(ctx, l.audience, in)
	assert.ErrorIs(t, err, agent.ErrReferenceMismatch, "a handoff for another operation")

	l.authorizer.err = errorx.New(errorx.KindPermissionDenied, "grant lacks agent:recover")
	in.Authority = "h1"
	_, err = l.service.Recover(ctx, l.audience, in)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "lifecycle authority without agent:recover")
}

// interleaving is the real execution store with a recovery and an
// activation committed once, right after the first read it answers:
// the moment a proof assembled from more than one read would mix two
// executions.
type interleaving struct {
	*agentpg.Store
	between func()
}

// after runs between once the first read has returned.
func (s *interleaving) after() {
	if s.between != nil {
		between := s.between
		s.between = nil
		between()
	}
}

func (s *interleaving) Introspect(ctx context.Context, agentGRN grn.GRN, c agent.Credential) (*agent.Snapshot, error) {
	defer s.after()
	return s.Store.Introspect(ctx, agentGRN, c)
}

func (s *interleaving) CredentialCurrent(ctx context.Context, agentGRN grn.GRN, c agent.Credential) (bool, error) {
	defer s.after()
	return s.Store.CredentialCurrent(ctx, agentGRN, c)
}

func (s *interleaving) GetExecution(ctx context.Context, agentGRN grn.GRN) (*agent.Execution, error) {
	defer s.after()
	return s.Store.GetExecution(ctx, agentGRN)
}

// TestExecutionService_IntrospectionNeverMixesTwoExecutions commits a
// recovery and a new activation between the service's store reads, and
// again after them. The old token was current with the old execution
// and the new generation is current with the new lineage; no answer may
// say both are current, because they never were together.
func TestExecutionService_IntrospectionNeverMixesTwoExecutions(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	first, err := l.service.Activate(ctx, l.audience, l.input(t, "r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)

	l.forwarded(t, "leaf-1")
	store := &interleaving{Store: l.exec}
	service := agent.NewExecutionService(l.agents, store, l.authorizer, l.operations, l.agents)
	next := generation(2)
	store.between = func() {
		body := `{"requestId":"rec-1","epoch":"1"}`
		l.handoff("h1", body)
		_, err := l.service.Recover(ctx, l.audience, agent.RecoveryInput{
			Agent: l.agent, RequestID: "rec-1", Epoch: l.epochNow(), Authority: "h1", Body: []byte(body),
		})
		require.NoError(t, err)
		_, err = l.service.Activate(ctx, l.audience, l.input(t, "r2", next, &first.Activation.ID, "leaf-2"))
		require.NoError(t, err)
	}

	during, err := service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-1"), &next)
	require.NoError(t, err)
	assert.False(t, during.Credential == agent.CredentialCurrent && during.Generation == agent.GenerationCurrent,
		"the old token and the new generation were never current together: %+v", during)
	assert.Equal(t, agent.CredentialCurrent, during.Credential, "the answer is the execution as it stood when read")
	assert.Equal(t, agent.GenerationNotCurrent, during.Generation)

	after, err := service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-1"), &next)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialFenced, after.Credential)
	assert.Equal(t, agent.GenerationCurrent, after.Generation)
	control, err := service.Introspect(ctx, l.audience, l.agent, l.forwarded(t, "leaf-2"), &next)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialCurrent, control.Credential, "control: a token of the new lineage and the new generation are current together")
	assert.Equal(t, agent.GenerationCurrent, control.Generation)
}
