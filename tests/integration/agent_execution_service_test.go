package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"math/big"
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

// The authority, verification and signing boundaries are substituted
// here; the stores behind the service are the real ones.

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

func (o *boundOperations) ResolveReference(_ context.Context, ref string) (*agent.OperationBinding, error) {
	if b, ok := o.refs[ref]; ok {
		return b, nil
	}
	return nil, errorx.New(errorx.KindPermissionDenied, "reference no longer authorized")
}

func (o *boundOperations) ResolveHandoff(_ context.Context, authority string) (*agent.OperationBinding, error) {
	if b, ok := o.handoffs[authority]; ok {
		return b, nil
	}
	return nil, errorx.New(errorx.KindNotFound, "handoff unknown")
}

// namedLeaves verifies a certificate by its PEM text, standing in for
// the chain check the CA boundary makes.
type namedLeaves map[string]agent.Credential

func (l namedLeaves) VerifyLeaf(_ context.Context, _ grn.GRN, certificatePEM []byte) (agent.Credential, error) {
	if c, ok := l[string(certificatePEM)]; ok {
		return c, nil
	}
	return agent.Credential{}, errorx.New(errorx.KindInvalidArgument, "certificate does not verify")
}

type countingSigner struct{ calls atomic.Int32 }

func (s *countingSigner) SignRequest(_ context.Context, _ grn.GRN, requestPEM []byte) (*agent.SignedCertificate, error) {
	s.calls.Add(1)
	serial := strings.TrimPrefix(string(requestPEM), "csr-")
	return &agent.SignedCertificate{Credential: valid(serial), CertificatePEM: "signed " + serial}, nil
}

// selfIssuer issues a parseable certificate with the next serial.
type selfIssuer struct{ next atomic.Int64 }

func (s *selfIssuer) Issue(_ context.Context, _ grn.GRN) (*agent.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(s.next.Add(1)),
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &agent.Certificate{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		NotAfter:       template.NotAfter,
	}, nil
}

type lifecycle struct {
	fence
	authorizer *grantingAuthorizer
	operations *boundOperations
	signer     *countingSigner
	issuer     *selfIssuer
	service    agent.ExecutionService
	audience   grn.GRN
}

func newLifecycle(t *testing.T) lifecycle {
	t.Helper()
	f := newFence(t)
	l := lifecycle{
		fence:      f,
		authorizer: &grantingAuthorizer{},
		operations: &boundOperations{refs: map[string]*agent.OperationBinding{}, handoffs: map[string]*agent.OperationBinding{}},
		signer:     &countingSigner{},
		issuer:     &selfIssuer{},
		audience:   scopedGRN("acme", agent.ServiceTypeOperator, "control"),
	}
	l.issuer.next.Store(1000)
	leaves := namedLeaves{"leaf-1": valid("1"), "leaf-2": valid("2"), "expired": expiredCredential("1")}
	l.service = agent.NewExecutionService(f.agents, f.exec, l.authorizer, l.operations, leaves, l.signer, l.issuer)
	return l
}

func expiredCredential(serial string) agent.Credential {
	c := valid(serial)
	c.NotAfter = time.Now().Add(-time.Second)
	return c
}

func (l lifecycle) input(requestID string, gen agent.Generation, replaces *agent.ActivationID, leaf string) agent.ActivationInput {
	return agent.ActivationInput{
		Agent: l.agent, RequestID: agent.RequestID(requestID), Epoch: l.epochNow(),
		Generation: gen, Replaces: replaces, CertificatePEM: []byte(leaf),
	}
}

func (l lifecycle) epochNow() int {
	a, err := l.agents.GetAssignment(context.Background(), l.agent)
	if err != nil {
		return 0
	}
	return a.Epoch()
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

	_, err := l.service.Activate(ctx, l.agent, l.input("r1", generation(1), nil, "leaf-1"))
	assert.ErrorIs(t, err, agent.ErrNotAnOperator, "an execution certificate cannot activate")

	l.authorizer.err = errorx.New(errorx.KindPermissionDenied, "no grant")
	_, err = l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	l.authorizer.err = errorx.New(errorx.KindUnavailable, "policy engine unreachable")
	_, err = l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "unknown freshness refuses")
	e, err := l.exec.GetExecution(ctx, l.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.ModeLegacy, e.Mode, "nothing was written by a refusal")

	l.authorizer.err = nil
	res, err := l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err, "control: the authorized audience activates")
	assert.True(t, res.Created)
	assert.Equal(t, agent.AdmissionAdmitted, l.admit(t, &res.Token, valid("1")))

	_, err = l.service.Activate(ctx, l.audience, l.input("r2", generation(2), &res.Activation.ID, "forged"))
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "a certificate that does not verify")
	oversized := l.input("r2", generation(2), &res.Activation.ID, strings.Repeat("x", agent.MaxCertificatePEM+1))
	calls := l.authorizer.calls.Load()
	_, err = l.service.Activate(ctx, l.audience, oversized)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	assert.Equal(t, calls, l.authorizer.calls.Load(), "an oversized certificate is refused before any authority is read")
}

func TestExecutionService_APendingRevisionNeedsAReferenceForThisAgentAndAssignment(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	first, err := l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)
	latest := &first.Activation.ID

	ref := func(s string) *string { return &s }
	attempt := func(name string, binding func(*agent.OperationBinding), want error) {
		t.Run(name, func(t *testing.T) {
			b := l.configureRef(name)
			if binding != nil {
				binding(b)
			}
			in := l.input("req-"+name, generation(len(name)+10), latest, "leaf-1")
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
	attempt("creation", func(b *agent.OperationBinding) { b.Kind = agent.OperationKindCreate }, agent.ErrUnsupportedReference)

	t.Run("a reference whose actor lost authority", func(t *testing.T) {
		in := l.input("revoked", generation(50), latest, "leaf-1")
		in.OperationRef = ref("unknown-or-revoked")
		_, err := l.service.Activate(ctx, l.audience, in)
		assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	})
	t.Run("control: a restart of the applied revision needs no reference", func(t *testing.T) {
		_, err := l.service.Activate(ctx, l.audience, l.input("restart", generation(51), latest, "leaf-1"))
		assert.NoError(t, err)
	})
}

func TestExecutionService_IntrospectionAnswersForThePresentedCertificate(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	g := func(n int) *agent.Generation { v := generation(n); return &v }

	in, err := l.service.Introspect(ctx, l.audience, l.agent, []byte("leaf-1"), nil)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialCurrent, in.Credential)
	assert.Equal(t, agent.GenerationNone, in.Generation, "before any activation")
	assert.Equal(t, agent.ModeLegacy, in.Mode)
	assert.Equal(t, l.controller, in.Assignee)

	expired, err := l.service.Introspect(ctx, l.audience, l.agent, []byte("expired"), nil)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialFenced, expired.Credential, "an expired leaf is fenced, not malformed")

	res, err := l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
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
		in, err := l.service.Introspect(ctx, l.audience, l.agent, []byte("leaf-1"), tc.supplied)
		require.NoError(t, err)
		assert.Equal(t, tc.want, in.Generation, tc.name)
	}

	require.NoError(t, l.service.Deactivate(ctx, l.audience, l.agent, res.Activation.ID))
	after, err := l.service.Introspect(ctx, l.audience, l.agent, []byte("leaf-1"), g(1))
	require.NoError(t, err)
	assert.Equal(t, agent.GenerationNotCurrent, after.Generation, "a deactivated generation")
	none, err := l.service.Introspect(ctx, l.audience, l.agent, []byte("leaf-1"), nil)
	require.NoError(t, err)
	assert.Equal(t, agent.GenerationNone, none.Generation)

	_, err = l.service.Introspect(ctx, l.agent, l.agent, []byte("leaf-1"), nil)
	assert.ErrorIs(t, err, agent.ErrNotAnOperator)
}

func (l lifecycle) handoff(authority, body string) {
	l.operations.handoffs[authority] = &agent.OperationBinding{
		Ref: "ref-" + authority, Kind: agent.OperationKindRecover, Org: orgRootGRN("acme"), Audience: l.audience,
		Target: l.agent, Assignee: l.controller, Epoch: l.epochNow(),
		BodySHA256: sha256.Sum256([]byte(body)), ExpiresAt: time.Now().Add(5 * time.Minute),
	}
}

func TestExecutionService_RecoveryIsBoundToItsExactBodyAndAnsweredOnce(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	res, err := l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)

	body := `{"request_id":"rec-1","certificate_request_pem":"csr-2"}`
	l.handoff("h1", body)
	in := agent.RecoveryInput{
		Agent: l.agent, RequestID: "rec-1", Epoch: l.epochNow(), Authority: "h1",
		Body: []byte(body + " "), CertificateRequestPEM: []byte("csr-2"),
	}
	_, err = l.service.Recover(ctx, l.audience, in)
	assert.ErrorIs(t, err, agent.ErrBodyMismatch)
	assert.Zero(t, l.signer.calls.Load(), "nothing is signed for a mismatched body")

	in.Body = []byte(body)
	recovered, err := l.service.Recover(ctx, l.audience, in)
	require.NoError(t, err, "control: the exact body recovers, the handoff was not spent")
	assert.Equal(t, "signed 2", recovered.CertificatePEM)
	assert.Equal(t, agent.AdmissionTokenStale, l.admit(t, &res.Token, valid("1")))

	again, err := l.service.Recover(ctx, l.audience, in)
	require.NoError(t, err)
	assert.Equal(t, recovered, again, "a lost response is answered with the recorded certificate")
	assert.Equal(t, int32(1), l.signer.calls.Load(), "and nothing is signed again")

	changed := `{"request_id":"rec-1","certificate_request_pem":"csr-3"}`
	l.handoff("h2", changed)
	replay := in
	replay.Authority, replay.Body, replay.CertificateRequestPEM = "h2", []byte(changed), []byte("csr-3")
	_, err = l.service.Recover(ctx, l.audience, replay)
	assert.ErrorIs(t, err, agent.ErrRecoveryConflict, "a completed request identifier with another binding is refused, not answered")
	assert.Equal(t, int32(1), l.signer.calls.Load())

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

func TestExecutionService_RenewalIssuesOnlyInsideTheCurrentExecution(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	res, err := l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)

	cert, err := l.service.Renew(ctx, l.agent, &res.Token, valid("1"))
	require.NoError(t, err, "the current generation renews")
	assert.NotEmpty(t, cert.CertificatePEM)

	stale, err := agent.NewToken()
	require.NoError(t, err)
	_, err = l.service.Renew(ctx, l.agent, &stale, valid("1"))
	assert.ErrorIs(t, err, agent.ErrExecutionTokenStale)
	_, err = l.service.Renew(ctx, l.agent, &res.Token, expiredCredential("1"))
	assert.ErrorIs(t, err, agent.ErrCredentialFenced)
	_, err = l.service.Renew(ctx, l.audience, &res.Token, valid("1"))
	assert.ErrorIs(t, err, agent.ErrNotTheAgent)
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
// again after them. The old certificate was current with the old
// execution and the new generation is current with the new lineage;
// no answer may say both are current, because they never were together.
func TestExecutionService_IntrospectionNeverMixesTwoExecutions(t *testing.T) {
	ctx := context.Background()
	l := newLifecycle(t)
	first, err := l.service.Activate(ctx, l.audience, l.input("r1", generation(1), nil, "leaf-1"))
	require.NoError(t, err)

	store := &interleaving{Store: l.exec}
	leaves := namedLeaves{"leaf-1": valid("1"), "leaf-2": valid("2")}
	service := agent.NewExecutionService(l.agents, store, l.authorizer, l.operations, leaves, l.signer, l.issuer)
	next := generation(2)
	store.between = func() {
		body := `{"request_id":"rec-1","certificate_request_pem":"csr-2"}`
		l.handoff("h1", body)
		_, err := l.service.Recover(ctx, l.audience, agent.RecoveryInput{
			Agent: l.agent, RequestID: "rec-1", Epoch: l.epochNow(), Authority: "h1",
			Body: []byte(body), CertificateRequestPEM: []byte("csr-2"),
		})
		require.NoError(t, err)
		_, err = l.service.Activate(ctx, l.audience, l.input("r2", next, &first.Activation.ID, "leaf-2"))
		require.NoError(t, err)
	}

	during, err := service.Introspect(ctx, l.audience, l.agent, []byte("leaf-1"), &next)
	require.NoError(t, err)
	assert.False(t, during.Credential == agent.CredentialCurrent && during.Generation == agent.GenerationCurrent,
		"the old certificate and the new generation were never current together: %+v", during)
	assert.Equal(t, agent.CredentialCurrent, during.Credential, "the answer is the execution as it stood when read")
	assert.Equal(t, agent.GenerationNotCurrent, during.Generation)

	after, err := service.Introspect(ctx, l.audience, l.agent, []byte("leaf-1"), &next)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialFenced, after.Credential)
	assert.Equal(t, agent.GenerationCurrent, after.Generation)
	control, err := service.Introspect(ctx, l.audience, l.agent, []byte("leaf-2"), &next)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialCurrent, control.Credential, "control: the recovered certificate and the new generation are current together")
	assert.Equal(t, agent.GenerationCurrent, control.Generation)
}
