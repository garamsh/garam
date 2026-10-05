package integration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// fence is one agent assigned to a controller, with the execution
// store over it.
type fence struct {
	p          *pgxpool.Pool
	agents     *agent.PostgresStore
	exec       *agentpg.Store
	agent      grn.GRN
	controller grn.GRN
}

// newFence is a legacy agent whose cutover is switched and that has not
// been activated: what every test below starts from, since a legacy
// agent's first activation waits for its switch (ADR-0086).
func newFence(t *testing.T) fence {
	t.Helper()
	f := newLegacyFence(t)
	f.switched(t, "fixture-cutover")
	return f
}

// newLegacyFence is a legacy agent no cutover has reached.
func newLegacyFence(t *testing.T) fence {
	t.Helper()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(context.Background(), org.NewOrganization(orgRootGRN("acme"), "acme")))
	agents := agent.NewPostgresStore(p)
	g := seedAgent(t, agents, orgRootGRN("acme"))
	controller := scopedGRN("acme", agent.ServiceTypeOperator, "controller")
	_, err := agents.Assign(context.Background(), g, controller)
	require.NoError(t, err)
	return fence{p: p, agents: agents, exec: agentpg.New(p), agent: g, controller: controller}
}

func generation(n int) agent.Generation { return agent.Generation(fmt.Sprintf("%032x", n)) }

// valid is a certificate of the agent inside its validity now.
func valid(serial string) agent.Credential {
	now := time.Now()
	return agent.Credential{Serial: serial, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
}

func authority(assignee grn.GRN) agent.Lifecycle {
	return agent.Lifecycle{Assignee: assignee, GrantExpiresAt: time.Now().Add(time.Hour)}
}

// activation is a request the authority admitted, minting the token it
// carries.
func (f fence) activation(t *testing.T, requestID string, gen agent.Generation, replaces *agent.ActivationID, c agent.Credential) (agent.ActivationRequest, agent.Token) {
	t.Helper()
	token, err := agent.NewToken()
	require.NoError(t, err)
	return agent.ActivationRequest{
		Agent:      f.agent,
		RequestID:  agent.RequestID(requestID),
		Generation: gen,
		Epoch:      f.epoch(t),
		Replaces:   replaces,
		Audience:   scopedGRN("acme", agent.ServiceTypeOperator, "control"),
		GrantID:    "grant-1",
		Authority:  authority(f.controller),
		Credential: c,
		TokenHash:  token.Hash(),
	}, token
}

func (f fence) epoch(t *testing.T) int {
	t.Helper()
	a, err := f.agents.GetAssignment(context.Background(), f.agent)
	require.NoError(t, err)
	return a.Epoch()
}

// activate applies a fresh activation and answers it with its token.
func (f fence) activate(t *testing.T, requestID string, gen agent.Generation, replaces *agent.ActivationID, c agent.Credential) (*agent.Activation, agent.Token) {
	t.Helper()
	req, token := f.activation(t, requestID, gen, replaces, c)
	a, created, err := f.exec.Activate(context.Background(), req)
	require.NoError(t, err)
	require.True(t, created)
	return a, token
}

// admitIn runs the fence inside tx, as a protected write runs it
// first in its own transaction; the lock it takes is held until tx
// ends.
func (f fence) admitIn(t *testing.T, tx pgx.Tx, token *agent.Token, c agent.Credential) agent.Admission {
	t.Helper()
	var hash []byte
	if token != nil {
		h := token.Hash()
		hash = h[:]
	}
	var answer string
	require.NoError(t, tx.QueryRow(context.Background(),
		`SELECT agent_execution_admit($1, $2, $3, $4, $5)`,
		f.agent.String(), hash, c.Serial, c.NotBefore, c.NotAfter).Scan(&answer))
	return agent.Admission(answer)
}

// admit runs the fence in a transaction of its own.
func (f fence) admit(t *testing.T, token *agent.Token, c agent.Credential) agent.Admission {
	t.Helper()
	tx, err := f.p.Begin(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	return f.admitIn(t, tx, token, c)
}

// awaitLockWaiter returns once some session of this database is
// waiting on a lock: the condition a paused writer or a queued
// replacement is in.
func awaitLockWaiter(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	require.Eventually(t, func() bool {
		var n int
		err := p.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n)
		return err == nil && n > 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestExecution_EveryAgentStartsLegacyAndATokenCannotSelectAnotherMode(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)

	e, err := f.exec.GetExecution(ctx, f.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.ModeLegacy, e.Mode)
	assert.Nil(t, e.Current)
	assert.Nil(t, e.Lineage)

	// An agent a claimed definition minted gets its row in the same
	// transaction as the agent row.
	operator := scopedGRN("acme", agent.ServiceTypeOperator, "claimant")
	d := defineAgent(t, f.p, f.agents, operator, agent.Values{})
	_, err = f.agents.Claim(ctx, d.Agent(), operator)
	require.NoError(t, err)
	claimed, err := f.exec.GetExecution(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.ModeLegacy, claimed.Mode)

	token, err := agent.NewToken()
	require.NoError(t, err)
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, valid("1")), "control: legacy admits without a token")
	assert.Equal(t, agent.AdmissionNotCutOver, f.admit(t, &token, valid("1")))
}

func TestExecution_ActivationFencesEveryOtherToken(t *testing.T) {
	f := newFence(t)

	a1, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t1, valid("1")))
	assert.Equal(t, agent.AdmissionTokenRequired, f.admit(t, nil, valid("1")))

	_, t2 := f.activate(t, "r2", generation(2), &a1.ID, valid("1"))
	assert.Equal(t, agent.AdmissionTokenStale, f.admit(t, &t1, valid("1")))
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, valid("1")), "control: the replacement's token is admitted")

	e, err := f.exec.GetExecution(context.Background(), f.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.ModeFenced, e.Mode)
	require.NotNil(t, e.Current)
	assert.Equal(t, generation(2), e.Current.Generation)
}

// TestExecution_ReplacementIsOrderedAgainstAdmittedWrites pauses a
// write after admission and before its commit, at the first cutover
// and at a later replacement. The replacement waits for the write to
// commit, and the same token is refused on the next write: no write of
// the old generation commits after its replacement did.
func TestExecution_ReplacementIsOrderedAgainstAdmittedWrites(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)

	// replace pauses a write of token after its admission, activates
	// generation over latest meanwhile, and answers the replacement and
	// its token once the paused write has committed.
	replace := func(t *testing.T, token *agent.Token, gen agent.Generation, latest *agent.ActivationID) (*agent.ActivationID, agent.Token) {
		tx, err := f.p.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		require.Equal(t, agent.AdmissionAdmitted, f.admitIn(t, tx, token, valid("1")))

		req, next := f.activation(t, "ordered-"+string(gen), gen, latest, valid("1"))
		done := make(chan error, 1)
		go func() {
			_, _, err := f.exec.Activate(ctx, req)
			done <- err
		}()
		awaitLockWaiter(t, f.p)
		select {
		case err := <-done:
			t.Fatalf("activation returned while an admitted write was open: %v", err)
		default:
		}
		_, err = tx.Exec(ctx, `UPDATE agents SET runtime_state = 'connected' WHERE grn = $1`, f.agent.String())
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		require.NoError(t, <-done)

		e, err := f.exec.GetExecution(ctx, f.agent)
		require.NoError(t, err)
		return e.Latest, next
	}

	latest, first := replace(t, nil, generation(1), nil)
	assert.Equal(t, agent.AdmissionTokenRequired, f.admit(t, nil, valid("1")), "first cutover: a legacy write is refused after it")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &first, valid("1")), "control: the activation is admitted")

	_, second := replace(t, &first, generation(2), latest)
	assert.Equal(t, agent.AdmissionTokenStale, f.admit(t, &first, valid("1")), "replacement: the old token is refused after it")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &second, valid("1")), "control: the replacement is admitted")
}

// TestExecution_AWriteWaitingOnAReplacementIsRefusedWhenItCommits
// pauses the replacement instead: a write arriving while the execution
// row is held FOR UPDATE waits, and reads the replaced token once the
// replacement commits.
func TestExecution_AWriteWaitingOnAReplacementIsRefusedWhenItCommits(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))

	// Deactivating through the store cannot be paused, so the test
	// holds the row as the store does and writes what it writes.
	replacing, err := f.p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = replacing.Rollback(ctx) }()
	_, err = replacing.Exec(ctx, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, f.agent.String())
	require.NoError(t, err)

	answer := make(chan agent.Admission, 1)
	go func() { answer <- f.admit(t, &t1, valid("1")) }()
	awaitLockWaiter(t, f.p)
	_, err = replacing.Exec(ctx, `UPDATE agent_executions SET activation_id = NULL, token_hash = NULL WHERE agent_grn = $1`, f.agent.String())
	require.NoError(t, err)
	_, err = replacing.Exec(ctx, `UPDATE agent_activations SET ended_reason = 'deactivated', ended_at = now() WHERE id = $1`, a1.ID.String())
	require.NoError(t, err)
	require.NoError(t, replacing.Commit(ctx))

	assert.Equal(t, agent.AdmissionTokenStale, <-answer)
}

func TestExecution_RetriesAnswerOneActivationAndRacesHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, _ := f.activate(t, "r1", generation(1), nil, valid("1"))

	t.Run("identical retries return the same activation with rising token versions", func(t *testing.T) {
		req, _ := f.activation(t, "r2", generation(2), &a1.ID, valid("1"))
		const retries = 4
		type outcome struct {
			a       *agent.Activation
			created bool
			token   agent.Token
			err     error
		}
		outcomes := make([]outcome, retries)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range retries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				token, err := agent.NewToken()
				if err != nil {
					outcomes[i].err = err
					return
				}
				r := req
				r.TokenHash = token.Hash()
				<-start
				a, created, err := f.exec.Activate(ctx, r)
				outcomes[i] = outcome{a: a, created: created, token: token, err: err}
			}()
		}
		close(start)
		wg.Wait()

		created := 0
		versions := map[int64]bool{}
		var newest int64
		for _, o := range outcomes {
			require.NoError(t, o.err)
			if o.created {
				created++
			}
			assert.Equal(t, outcomes[0].a.ID, o.a.ID)
			versions[o.a.TokenVersion] = true
			newest = max(newest, o.a.TokenVersion)
		}
		assert.Equal(t, 1, created)
		assert.Len(t, versions, retries)
		for _, o := range outcomes {
			want := agent.AdmissionTokenStale
			if o.a.TokenVersion == newest {
				want = agent.AdmissionAdmitted
			}
			assert.Equal(t, want, f.admit(t, &o.token, valid("1")), "only the highest token version is admitted")
		}
	})

	e, err := f.exec.GetExecution(ctx, f.agent)
	require.NoError(t, err)

	t.Run("a changed request under the same identifier conflicts", func(t *testing.T) {
		req, _ := f.activation(t, "r2", generation(3), &a1.ID, valid("1"))
		_, _, err := f.exec.Activate(ctx, req)
		assert.ErrorIs(t, err, agent.ErrActivationConflict)
	})

	t.Run("different requests on one anchor: one wins, the rest are stale", func(t *testing.T) {
		const contenders = 4
		errs := make([]error, contenders)
		reqs := make([]agent.ActivationRequest, contenders)
		for i := range contenders {
			reqs[i], _ = f.activation(t, fmt.Sprintf("race-%d", i), generation(10+i), e.Latest, valid("1"))
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range contenders {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _, errs[i] = f.exec.Activate(ctx, reqs[i])
			}()
		}
		close(start)
		wg.Wait()
		won := 0
		for _, err := range errs {
			if err == nil {
				won++
				continue
			}
			assert.ErrorIs(t, err, agent.ErrActivationStale)
		}
		assert.Equal(t, 1, won)
	})

	t.Run("an ended activation is never revived by a retry", func(t *testing.T) {
		req, _ := f.activation(t, "r2", generation(2), &a1.ID, valid("1"))
		_, _, err := f.exec.Activate(ctx, req)
		assert.ErrorIs(t, err, agent.ErrActivationEnded)
	})
}

func TestExecution_AGenerationIsActivatedOncePerLineage(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, _ := f.activate(t, "r1", generation(1), nil, valid("1"))
	a2, _ := f.activate(t, "r2", generation(2), &a1.ID, valid("1"))

	req, _ := f.activation(t, "r3", generation(1), &a2.ID, valid("1"))
	_, _, err := f.exec.Activate(ctx, req)
	assert.ErrorIs(t, err, agent.ErrGenerationUsed)

	control, _ := f.activation(t, "r4", generation(3), &a2.ID, valid("1"))
	_, created, err := f.exec.Activate(ctx, control)
	require.NoError(t, err, "control: a new generation on the same anchor activates")
	assert.True(t, created)
}

func TestExecution_DeactivationEndsOnlyTheActivationItNames(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))

	ended, err := f.exec.Deactivate(ctx, f.agent, a1.ID, authority(f.controller))
	require.NoError(t, err)
	assert.True(t, ended)
	assert.Equal(t, agent.AdmissionTokenStale, f.admit(t, &t1, valid("1")))

	// The anchor stays the deactivated activation, so a replay of the
	// first activation's anchor is stale.
	stale, _ := f.activation(t, "r-stale", generation(9), nil, valid("1"))
	_, _, err = f.exec.Activate(ctx, stale)
	assert.ErrorIs(t, err, agent.ErrActivationStale)

	a2, t2 := f.activate(t, "r2", generation(2), &a1.ID, valid("1"))
	ended, err = f.exec.Deactivate(ctx, f.agent, a1.ID, authority(f.controller))
	require.NoError(t, err)
	assert.False(t, ended, "a replay of an ended activation is a no-op")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, valid("1")), "the successor survives the replay")

	_, err = f.exec.Deactivate(ctx, f.agent, agent.ActivationID{1}, authority(f.controller))
	assert.ErrorIs(t, err, agent.ErrActivationNotFound)

	other := seedAgent(t, f.agents, orgRootGRN("acme"))
	_, err = f.agents.Assign(ctx, other, f.controller)
	require.NoError(t, err)
	_, err = f.exec.Deactivate(ctx, other, a2.ID, authority(f.controller))
	assert.ErrorIs(t, err, agent.ErrActivationNotFound, "another agent's activation is not found")

	_, err = f.exec.Deactivate(ctx, f.agent, a2.ID, authority(scopedGRN("acme", agent.ServiceTypeOperator, "elsewhere")))
	assert.ErrorIs(t, err, agent.ErrNotAssigned)
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, valid("1")))
}

func (f fence) recovery(requestID, ref string, digest byte, c agent.Credential) agent.RecoveryRequest {
	return agent.RecoveryRequest{
		Agent:          f.agent,
		RequestID:      agent.RequestID(requestID),
		Authority:      authority(f.controller),
		TokenExpiresAt: time.Now().Add(5 * time.Minute),
		OperationRef:   ref,
		BodySHA256:     [32]byte{digest},
		Lineage:        agent.NewLineage(),
		Credential:     c,
		CertificatePEM: "certificate " + c.Serial,
	}
}

func TestExecution_RecoveryFencesTheLegacyCohortAndEveryOlderLineage(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))
	require.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t1, valid("1")))

	first := f.recovery("rec-1", "ref-1", 1, valid("2"))
	first.Epoch = f.epoch(t)
	recovered, err := f.exec.Recover(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, first.Lineage, recovered.Lineage)
	assert.Equal(t, agent.AdmissionTokenStale, f.admit(t, &t1, valid("1")), "recovery ends the current activation")

	// The same generation activates again in the new lineage.
	a2, t2 := f.activate(t, "r2", generation(1), &a1.ID, valid("2"))
	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t2, valid("1")), "the legacy cohort is fenced")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, valid("2")), "control: the recovered credential is admitted")

	old, err := f.exec.CredentialCurrent(ctx, f.agent, valid("1"))
	require.NoError(t, err)
	assert.False(t, old)

	t.Run("a lost response is answered with the recorded certificate", func(t *testing.T) {
		retry := first
		retry.Lineage = agent.NewLineage()
		retry.CertificatePEM = "another"
		got, err := f.exec.Recover(ctx, retry)
		require.NoError(t, err)
		assert.Equal(t, first.Lineage, got.Lineage)
		assert.Equal(t, first.CertificatePEM, got.CertificatePEM)
	})
	t.Run("a changed body is refused", func(t *testing.T) {
		changed := first
		changed.BodySHA256 = [32]byte{2}
		_, err := f.exec.Recover(ctx, changed)
		assert.ErrorIs(t, err, agent.ErrRecoveryConflict)
	})
	t.Run("a reference recovers once", func(t *testing.T) {
		again := f.recovery("rec-2", "ref-1", 1, valid("3"))
		again.Epoch = f.epoch(t)
		_, err := f.exec.Recover(ctx, again)
		assert.ErrorIs(t, err, agent.ErrRecoveryConflict)
	})

	second := f.recovery("rec-3", "ref-2", 3, valid("3"))
	second.Epoch = f.epoch(t)
	_, err = f.exec.Recover(ctx, second)
	require.NoError(t, err)
	_, t3 := f.activate(t, "r3", generation(3), &a2.ID, valid("3"))
	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t3, valid("2")), "an older tracked lineage is fenced")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t3, valid("3")))
}

func TestExecution_RenewalStaysInTheLineageItWasAdmittedIn(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))

	// In the legacy cohort a renewal records nothing and any
	// certificate of the agent stays admitted.
	require.NoError(t, f.exec.RecordRenewal(ctx, fencing(f.agent, &t1, valid("1")), valid("2")))
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t1, valid("2")))

	rec := f.recovery("rec-1", "ref-1", 1, valid("10"))
	rec.Epoch = f.epoch(t)
	_, err := f.exec.Recover(ctx, rec)
	require.NoError(t, err)
	_, t2 := f.activate(t, "r2", generation(2), &a1.ID, valid("10"))

	require.NoError(t, f.exec.RecordRenewal(ctx, fencing(f.agent, &t2, valid("10")), valid("11")))
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, valid("11")), "the successor joins the lineage")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, valid("10")), "the predecessor stays valid until it expires")
	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t2, valid("12")), "control: an unrecorded serial is not")

	err = f.exec.RecordRenewal(ctx, fencing(f.agent, &t1, valid("10")), valid("13"))
	assert.ErrorIs(t, err, agent.ErrExecutionTokenStale)
	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t2, valid("13")), "a refused renewal records nothing")
}

// fencing is what a request of agentGRN presenting c and token offers
// the execution fence.
func fencing(agentGRN grn.GRN, token *agent.Token, c agent.Credential) agent.Fencing {
	f := agent.Fencing{Agent: agentGRN, Credential: c}
	if token != nil {
		h := token.Hash()
		f.Token = &h
	}
	return f
}

func TestExecution_ReassignmentEndsTheActivationAndIsOrderedAgainstWrites(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	_, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))
	staleEpoch := f.epoch(t)

	tx, err := f.p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.Equal(t, agent.AdmissionAdmitted, f.admitIn(t, tx, &t1, valid("1")))

	successor := scopedGRN("acme", agent.ServiceTypeOperator, "successor")
	done := make(chan error, 1)
	go func() {
		_, err := f.agents.Assign(ctx, f.agent, successor)
		done <- err
	}()
	awaitLockWaiter(t, f.p)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-done)

	assert.Equal(t, agent.AdmissionTokenStale, f.admit(t, &t1, valid("1")))

	e, err := f.exec.GetExecution(ctx, f.agent)
	require.NoError(t, err)
	req, _ := f.activation(t, "r2", generation(2), e.Latest, valid("1"))
	_, _, err = f.exec.Activate(ctx, req)
	assert.ErrorIs(t, err, agent.ErrNotAssigned, "the replaced assignee no longer activates")

	req.Authority = authority(successor)
	req.Epoch = staleEpoch
	_, _, err = f.exec.Activate(ctx, req)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	req.Epoch = f.epoch(t)
	_, created, err := f.exec.Activate(ctx, req)
	require.NoError(t, err, "control: the new assignee at the current epoch activates")
	assert.True(t, created)
}

// TestExecution_ValidityIsJudgedWhenTheLockIsGranted holds the
// execution row while a write and an activation wait on it, and lets
// the certificate and the grant they carry expire in between. Both are
// refused when the lock is granted, though both were valid when they
// started; the same waits with time to spare are admitted.
func TestExecution_ValidityIsJudgedWhenTheLockIsGranted(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	_, t1 := f.activate(t, "r1", generation(1), nil, valid("1"))

	expired := valid("1")
	expired.NotAfter = time.Now().Add(-time.Second)
	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t1, expired))
	notYet := valid("1")
	notYet.NotBefore = time.Now().Add(time.Minute)
	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t1, notYet))

	hold := func(t *testing.T) pgx.Tx {
		tx, err := f.p.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, f.agent.String())
		require.NoError(t, err)
		return tx
	}

	for _, tc := range []struct {
		name   string
		margin time.Duration
		want   agent.Admission
	}{
		{name: "expires while waiting", margin: 300 * time.Millisecond, want: agent.AdmissionCredentialFenced},
		{name: "control: valid after the wait", margin: time.Hour, want: agent.AdmissionAdmitted},
	} {
		t.Run("write: "+tc.name, func(t *testing.T) {
			tx := hold(t)
			defer func() { _ = tx.Rollback(ctx) }()
			c := valid("1")
			c.NotAfter = time.Now().Add(tc.margin)
			answer := make(chan agent.Admission, 1)
			go func() { answer <- f.admit(t, &t1, c) }()
			awaitLockWaiter(t, f.p)
			waitPast(c.NotAfter, tc.margin)
			require.NoError(t, tx.Rollback(ctx))
			assert.Equal(t, tc.want, <-answer)
		})
	}

	for i, tc := range []struct {
		name   string
		margin time.Duration
		want   error
	}{
		{name: "grant expires while waiting", margin: 300 * time.Millisecond, want: agent.ErrAuthorityExpired},
		{name: "control: grant valid after the wait", margin: time.Hour},
	} {
		t.Run("activation: "+tc.name, func(t *testing.T) {
			e, err := f.exec.GetExecution(ctx, f.agent)
			require.NoError(t, err)
			req, _ := f.activation(t, fmt.Sprintf("late-%d", i), generation(20+i), e.Latest, valid("1"))
			req.Authority.GrantExpiresAt = time.Now().Add(tc.margin)
			tx := hold(t)
			defer func() { _ = tx.Rollback(ctx) }()
			done := make(chan error, 1)
			go func() {
				_, _, err := f.exec.Activate(ctx, req)
				done <- err
			}()
			awaitLockWaiter(t, f.p)
			waitPast(req.Authority.GrantExpiresAt, tc.margin)
			require.NoError(t, tx.Rollback(ctx))
			err = <-done
			if tc.want == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

// waitPast lets deadline pass when the case is the one that expires;
// a control with an hour to spare does not wait.
func waitPast(deadline time.Time, margin time.Duration) {
	if margin < time.Minute {
		time.Sleep(time.Until(deadline) + 50*time.Millisecond)
	}
}

// TestExecution_NoPathHoldsALockWhileAwaitingAnotherConnection runs
// every operation that changes an execution over a pool of one
// connection. A path that held the execution lock and asked the pool
// for a second connection would wait on itself until the deadline.
func TestExecution_NoPathHoldsALockWhileAwaitingAnotherConnection(t *testing.T) {
	f := newFence(t)
	cfg := f.p.Config().Copy()
	cfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(single.Close)
	exec := agentpg.New(single)
	agents := agent.NewPostgresStore(single)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, t1 := f.activation(t, "r1", generation(1), nil, valid("1"))
	a1, _, err := exec.Activate(ctx, req)
	require.NoError(t, err)
	_, _, err = exec.Activate(ctx, req)
	require.NoError(t, err, "a retry reminting the token")
	require.NoError(t, exec.RecordRenewal(ctx, fencing(f.agent, &t1, valid("1")), valid("2")))
	_, err = exec.Deactivate(ctx, f.agent, a1.ID, authority(f.controller))
	require.NoError(t, err)
	rec := f.recovery("rec-1", "ref-1", 1, valid("3"))
	rec.Epoch = f.epoch(t)
	_, err = exec.Recover(ctx, rec)
	require.NoError(t, err)
	_, err = exec.GetExecution(ctx, f.agent)
	require.NoError(t, err)
	_, err = exec.CredentialCurrent(ctx, f.agent, valid("3"))
	require.NoError(t, err)
	_, err = agents.Assign(ctx, f.agent, f.controller)
	require.NoError(t, err)
	assert.NoError(t, ctx.Err())
}

func TestExecution_AnAgentNobodyHoldsIsUnknownToTheFence(t *testing.T) {
	f := newFence(t)
	stranger := fence{p: f.p, agent: scopedGRN("acme", agent.ServiceType, "nobody")}
	assert.Equal(t, agent.AdmissionUnknownAgent, stranger.admit(t, nil, valid("1")))
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, valid("1")), "control: a held agent is admitted")
	_, err := f.exec.GetExecution(context.Background(), stranger.agent)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}
