package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// The cutover stages against the real stores (ADR-0086). The authority
// a stage rests on is what the store rechecks under its locks; the
// machine routes and the authority that admits them are exercised in
// agent_cutover_route_test.go.

// newCutover is an agent its operator claimed from a definition
// carrying values, with the execution store over it.
func newCutover(t *testing.T, values agent.Values) fence {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	agents := agent.NewPostgresStore(p)
	controller := scopedGRN("acme", agent.ServiceTypeOperator, "controller")
	d := defineAgent(t, p, agents, controller, values)
	_, err := agents.Claim(ctx, d.Agent(), controller)
	require.NoError(t, err)
	return fence{p: p, agents: agents, exec: agentpg.New(p), agent: d.Agent(), controller: controller}
}

// stage is a cutover stage of importID as the authority admitted it.
func (f fence) stage(importID string) agent.CutoverRequest {
	return agent.CutoverRequest{
		Agent: f.agent, ImportID: agent.ImportID(importID),
		Audience: scopedGRN("acme", agent.ServiceTypeOperator, "control"),
		GrantID:  "grant-1", OperationRef: "ref-1", Authority: authority(f.controller),
	}
}

// freezing is a freeze of importID carrying digest.
func (f fence) freezing(t *testing.T, importID string, digest agent.Digest) agent.FreezeRequest {
	t.Helper()
	return agent.FreezeRequest{CutoverRequest: f.stage(importID), Epoch: f.epoch(t), SourceDigest: digest}
}

// imported is the digest the Operator's import verifies: the one the
// agent's cutover answers.
func (f fence) imported(t *testing.T) agent.Digest {
	t.Helper()
	c, err := f.exec.GetCutover(context.Background(), f.agent)
	require.NoError(t, err)
	require.NotNil(t, c.SourceDigest)
	return *c.SourceDigest
}

// frozen freezes importID over the imported digest.
func (f fence) frozen(t *testing.T, importID string) *agent.CutoverAttempt {
	t.Helper()
	a, created, err := f.exec.Freeze(context.Background(), f.freezing(t, importID, f.imported(t)))
	require.NoError(t, err)
	require.True(t, created)
	return a
}

// switched freezes and switches importID.
func (f fence) switched(t *testing.T, importID string) *agent.CutoverAttempt {
	t.Helper()
	f.frozen(t, importID)
	a, err := f.exec.Switch(context.Background(), f.stage(importID))
	require.NoError(t, err)
	return a
}

// listed reports whether the agent's definition is in what its operator
// polls.
func (f fence) listed(t *testing.T) bool {
	t.Helper()
	states, err := f.agents.ListDefinitionsByOperator(context.Background(), f.controller)
	require.NoError(t, err)
	for _, s := range states {
		if s.Definition().Agent() == f.agent {
			return true
		}
	}
	return false
}

// switchingIn takes the agent's execution row FOR UPDATE in tx and
// marks its attempt switched there, as a switch holds both until it
// commits.
func (f fence) switchingIn(t *testing.T, tx pgx.Tx, importID string) {
	t.Helper()
	ctx := context.Background()
	_, err := tx.Exec(ctx, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, f.agent.String())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE agent_cutovers SET stage = 'switched', switched_at = clock_timestamp()
		WHERE agent_grn = $1 AND import_id = $2`, f.agent.String(), importID)
	require.NoError(t, err)
}

func TestCutover_ADigestThatIsNotTheSourceFreezesNothing(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "large", "region": "eu"})

	c, err := f.exec.GetCutover(ctx, f.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.CutoverEligible, c.Class)
	require.NotNil(t, c.Source)
	assert.Equal(t, agent.Values{"model": "large", "region": "eu"}, c.Source.Values, "every value verbatim")
	assert.Equal(t, agent.SourceDigest(f.agent, f.controller, c.Source.Values), *c.SourceDigest)
	assert.Nil(t, c.Attempt)

	stale := agent.SourceDigest(f.agent, f.controller, agent.Values{"model": "small", "region": "eu"})
	_, _, err = f.exec.Freeze(ctx, f.freezing(t, "import-1", stale))
	assert.ErrorIs(t, err, agent.ErrCutoverDigestMismatch)
	c, err = f.exec.GetCutover(ctx, f.agent)
	require.NoError(t, err)
	assert.Nil(t, c.Attempt, "nothing is frozen")
	assert.True(t, f.listed(t), "and the operator still polls it")

	// The control: the Operator imports again and freezes what it read.
	a, created, err := f.exec.Freeze(ctx, f.freezing(t, "import-1", f.imported(t)))
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, agent.CutoverFrozen, a.Stage)
	assert.False(t, f.listed(t), "a frozen definition is no longer polled")
}

func TestCutover_AnAgentNoDefinitionDescribesCutsOverWithEmptyValues(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFence(t)

	c, err := f.exec.GetCutover(ctx, f.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.CutoverEligible, c.Class)
	assert.Nil(t, c.Source)
	require.NotNil(t, c.SourceDigest)
	assert.Equal(t, agent.SourceDigest(f.agent, f.controller, agent.Values{}), *c.SourceDigest,
		"the digest covers the assignee and no value")

	f.switched(t, "import-1")
	_, created, err := f.exec.Activate(ctx, first(t, f))
	require.NoError(t, err)
	assert.True(t, created, "it activates once switched")
}

// first is the agent's first activation request.
func first(t *testing.T, f fence) agent.ActivationRequest {
	t.Helper()
	req, _ := f.activation(t, "first", generation(1), nil, valid("1"))
	return req
}

// TestCutover_ALegacyAgentActivatesOnlyOnceItsSwitchIsRecorded holds
// a legacy agent's first activation behind the switch: refused with no
// attempt and with one frozen, admitted once switched.
func TestCutover_ALegacyAgentActivatesOnlyOnceItsSwitchIsRecorded(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "large"})

	_, _, err := f.exec.Activate(ctx, first(t, f))
	assert.ErrorIs(t, err, agent.ErrCutoverRequired, "nothing imported")
	f.frozen(t, "import-1")
	_, _, err = f.exec.Activate(ctx, first(t, f))
	assert.ErrorIs(t, err, agent.ErrCutoverRequired, "frozen, not switched")

	_, err = f.exec.Switch(ctx, f.stage("import-1"))
	require.NoError(t, err)
	_, created, err := f.exec.Activate(ctx, first(t, f))
	require.NoError(t, err)
	assert.True(t, created)
	e, err := f.exec.GetExecution(ctx, f.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.ModeFenced, e.Mode)
}

// TestCutover_AnActivationQueuedBehindASwitchReadsIt queues a first
// activation behind a switch holding the execution row: rolled back,
// the activation is refused; committed, it is admitted. Either way the
// activation decides on what the lock left, not on what it read before
// waiting.
func TestCutover_AnActivationQueuedBehindASwitchReadsIt(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{})
	f.frozen(t, "import-1")

	queued := func(commit bool) error {
		tx, err := f.p.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		f.switchingIn(t, tx, "import-1")
		done := make(chan error, 1)
		go func() {
			_, _, err := f.exec.Activate(ctx, first(t, f))
			done <- err
		}()
		awaitLockWaiter(t, f.p)
		if commit {
			require.NoError(t, tx.Commit(ctx))
		} else {
			require.NoError(t, tx.Rollback(ctx))
		}
		return <-done
	}

	assert.ErrorIs(t, queued(false), agent.ErrCutoverRequired, "control: a switch that never commits admits nothing")
	assert.NoError(t, queued(true), "a switch committing first is seen")
}

// TestCutover_TheOperatorsCertificateRouteIsClosedOnceTheSwitchCommits
// queues the legacy route's decision behind a switch holding the
// execution row, as TestCutover_AnActivationQueuedBehindASwitchReadsIt
// queues an activation.
func TestCutover_TheOperatorsCertificateRouteIsClosedOnceTheSwitchCommits(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{})
	require.NoError(t, f.agents.AdmitLegacyIssuance(ctx, f.agent), "control: a legacy agent's operator is answered")
	f.frozen(t, "import-1")
	require.NoError(t, f.agents.AdmitLegacyIssuance(ctx, f.agent), "a freeze leaves the route open")

	queued := func(commit bool) error {
		tx, err := f.p.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		f.switchingIn(t, tx, "import-1")
		done := make(chan error, 1)
		go func() { done <- f.agents.AdmitLegacyIssuance(ctx, f.agent) }()
		awaitLockWaiter(t, f.p)
		if commit {
			require.NoError(t, tx.Commit(ctx))
		} else {
			require.NoError(t, tx.Rollback(ctx))
		}
		return <-done
	}
	assert.NoError(t, queued(false), "control: a switch that never commits closes nothing")
	assert.ErrorIs(t, queued(true), agent.ErrCutOver)
}

// TestCutover_FreezeIsOrderedAgainstWhatHoldsItsRows queues a freeze
// behind each legacy write that holds a row it takes: a message write
// admitted through the execution fence, a rename and a claim holding
// the definition row. Each commits first, and the freeze then succeeds
// over what it left.
func TestCutover_FreezeIsOrderedAgainstWhatHoldsItsRows(t *testing.T) {
	ctx := context.Background()
	holders := map[string]func(t *testing.T, f fence, tx pgx.Tx){
		"an admitted write": func(t *testing.T, f fence, tx pgx.Tx) {
			require.Equal(t, agent.AdmissionAdmitted, f.admitIn(t, tx, nil, valid("1")))
		},
		"a rename": func(t *testing.T, f fence, tx pgx.Tx) {
			_, err := tx.Exec(ctx, `UPDATE agent_definitions SET name = 'Renamed' WHERE agent_grn = $1`, f.agent.String())
			require.NoError(t, err)
		},
		"a claim": func(t *testing.T, f fence, tx pgx.Tx) {
			_, err := tx.Exec(ctx, `SELECT 1 FROM agent_definitions WHERE agent_grn = $1 FOR UPDATE`, f.agent.String())
			require.NoError(t, err)
		},
	}
	for name, hold := range holders {
		t.Run(name, func(t *testing.T) {
			f := newCutover(t, agent.Values{"model": "large"})
			digest := f.imported(t)
			tx, err := f.p.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			hold(t, f, tx)

			done := make(chan error, 1)
			go func() {
				_, _, err := f.exec.Freeze(ctx, f.freezing(t, "import-1", digest))
				done <- err
			}()
			awaitLockWaiter(t, f.p)
			select {
			case err := <-done:
				t.Fatalf("the freeze did not wait for the write holding its row: %v", err)
			default:
			}
			require.NoError(t, tx.Commit(ctx))
			require.NoError(t, <-done)
		})
	}

	// After the freeze, a legacy write is still admitted and a rename
	// still lands, while a claim stays refused.
	f := newCutover(t, agent.Values{"model": "large"})
	f.frozen(t, "import-1")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, valid("1")), "a freeze changes no admission")
	renamed, err := f.agents.RenameDefinition(ctx, f.agent, agent.Name("Renamed"))
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Renamed"), renamed.Name)
	require.NotNil(t, renamed.Cutover)
	assert.Equal(t, agent.CutoverFrozen, *renamed.Cutover)
	_, err = f.agents.Claim(ctx, f.agent, f.controller)
	assert.ErrorIs(t, err, agent.ErrDefinitionClaimed)
	_, _, err = f.exec.Freeze(ctx, f.freezing(t, "import-1", f.imported(t)))
	assert.NoError(t, err, "the name is not in the digest, so a rename moves no freeze")
}

// TestCutover_AnUnclaimedDefinitionHasNothingToFreeze: no execution row
// exists to lock until its operator claims it.
func TestCutover_AnUnclaimedDefinitionHasNothingToFreeze(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{})
	unclaimed := defineAgent(t, f.p, f.agents, f.controller, agent.Values{"model": "large"})
	other := f
	other.agent = unclaimed.Agent()

	_, err := f.exec.GetCutover(ctx, other.agent)
	assert.ErrorIs(t, err, agent.ErrNotAssigned)
	_, _, err = f.exec.Freeze(ctx, agent.FreezeRequest{CutoverRequest: other.stage("import-1"), Epoch: 1})
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)
	_, err = f.agents.Claim(ctx, other.agent, f.controller)
	assert.NoError(t, err, "and its claim is untouched")

	// The control: the claimed one beside it freezes.
	f.frozen(t, "import-1")
}

// TestCutover_StoredValuesThatDoNotParseArePreservedAndBlock covers a
// payload garam would refuse to write today: it is reported, kept as
// stored, and frozen by no digest.
func TestCutover_StoredValuesThatDoNotParseArePreservedAndBlock(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "large"})
	const stored = `{"tools.pins.git": "v2", "replicas": 2}`
	withDefinitionsOpen(t, f.p, func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, `UPDATE agent_definitions SET agent_values = $2 WHERE agent_grn = $1`, f.agent.String(), stored)
		require.NoError(t, err)
	})

	c, err := f.exec.GetCutover(ctx, f.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.CutoverUnparseable, c.Class)
	assert.Nil(t, c.Source)
	assert.Nil(t, c.SourceDigest)
	for _, digest := range []agent.Digest{
		agent.SourceDigest(f.agent, f.controller, agent.Values{}),
		agent.SourceDigest(f.agent, f.controller, agent.Values{"model": "large"}),
	} {
		_, _, err = f.exec.Freeze(ctx, f.freezing(t, "import-1", digest))
		assert.ErrorIs(t, err, agent.ErrCutoverNotEligible)
	}
	var kept string
	require.NoError(t, f.p.QueryRow(ctx, `SELECT agent_values::text FROM agent_definitions WHERE agent_grn = $1`,
		f.agent.String()).Scan(&kept))
	assert.JSONEq(t, stored, kept, "the payload is preserved as stored")

	// The control: the same agent with a payload that parses freezes.
	withDefinitionsOpen(t, f.p, func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, `UPDATE agent_definitions SET agent_values = '{"model": "large"}' WHERE agent_grn = $1`, f.agent.String())
		require.NoError(t, err)
	})
	f.frozen(t, "import-1")
}

// TestCutover_RollbackEndsOnlyAFrozenAttempt walks one agent through
// two attempts. Rolling back the first lists its definition again; no
// request reaches it afterwards, and none undoes the second, which
// once switched is not rolled back at all.
func TestCutover_RollbackEndsOnlyAFrozenAttempt(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "large"})

	f.frozen(t, "import-1")
	require.False(t, f.listed(t))
	ended, err := f.exec.RollBack(ctx, f.stage("import-1"))
	require.NoError(t, err)
	assert.Equal(t, agent.CutoverRolledBack, ended.Stage)
	assert.NotNil(t, ended.EndedAt)
	assert.True(t, f.listed(t), "a rolled-back definition is polled again")

	_, err = f.exec.RollBack(ctx, f.stage("import-1"))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptEnded)
	_, _, err = f.exec.Freeze(ctx, f.freezing(t, "import-1", f.imported(t)))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptEnded, "a late replay of the ended freeze")
	_, err = f.exec.Switch(ctx, f.stage("import-1"))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptEnded)

	f.frozen(t, "import-2")
	_, err = f.exec.RollBack(ctx, f.stage("import-1"))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptEnded, "the first attempt's request reaches nothing")
	_, err = f.exec.RollBack(ctx, f.stage("import-3"))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptOpen, "an import nobody froze while another is open")
	_, _, err = f.exec.Freeze(ctx, f.freezing(t, "import-3", f.imported(t)))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptOpen)

	_, err = f.exec.Switch(ctx, f.stage("import-2"))
	require.NoError(t, err)
	_, err = f.exec.RollBack(ctx, f.stage("import-2"))
	assert.ErrorIs(t, err, agent.ErrReverseMigrationRequired)
	_, _, err = f.exec.Activate(ctx, first(t, f))
	require.NoError(t, err)
	_, err = f.exec.RollBack(ctx, f.stage("import-2"))
	assert.ErrorIs(t, err, agent.ErrReverseMigrationRequired, "nor once the agent is fenced")
	assert.False(t, f.listed(t))

	// An import never frozen, with none open, is unknown.
	g := newCutover(t, agent.Values{})
	_, err = g.exec.Switch(ctx, g.stage("import-9"))
	assert.ErrorIs(t, err, agent.ErrCutoverAttemptNotFound)
}

// TestCutover_ARetryAnswersWhatItsStageRecorded replays each stage, as
// a caller that lost the answer does, and again through a store a
// restart built: one attempt, one GRN, one assignment.
func TestCutover_ARetryAnswersWhatItsStageRecorded(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "large"})
	epoch := f.epoch(t)

	frozen := f.frozen(t, "import-1")
	again, created, err := f.exec.Freeze(ctx, f.freezing(t, "import-1", f.imported(t)))
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, frozen, again)
	_, _, err = f.exec.Freeze(ctx, f.freezing(t, "import-1", agent.Digest{}))
	assert.ErrorIs(t, err, agent.ErrCutoverDigestMismatch, "a replay carrying another digest is not the same freeze")

	restarted := agentpg.New(f.p)
	switched, err := restarted.Switch(ctx, f.stage("import-1"))
	require.NoError(t, err)
	assert.Equal(t, agent.CutoverSwitched, switched.Stage)
	replayed, err := agentpg.New(f.p).Switch(ctx, f.stage("import-1"))
	require.NoError(t, err)
	assert.Equal(t, switched, replayed)
	_, created, err = restarted.Freeze(ctx, f.freezing(t, "import-1", f.imported(t)))
	require.NoError(t, err)
	assert.False(t, created, "a freeze replayed after the switch answers the attempt")

	c, err := restarted.GetCutover(ctx, f.agent)
	require.NoError(t, err)
	require.NotNil(t, c.Attempt)
	assert.Equal(t, agent.ImportID("import-1"), c.Attempt.ImportID)
	assert.Equal(t, agent.CutoverSwitched, c.Attempt.Stage)
	assert.Equal(t, f.agent, c.Agent)
	assert.Equal(t, epoch, c.Epoch, "no stage moves the assignment")
	assert.Equal(t, 1, countRows(t, f.p, `SELECT count(*) FROM agent_cutovers WHERE agent_grn = $1`, f.agent.String()))
	assert.Equal(t, 1, countRows(t, f.p, `SELECT count(*) FROM agents WHERE org_grn = $1`, orgRootGRN("acme").String()))
}

// TestCutover_ASwitchedDefinitionIsArchivalAndPreserved: the values
// stay as defined and the browser read says where the source went,
// while the agent keeps running on what it was built from.
func TestCutover_ASwitchedDefinitionIsArchivalAndPreserved(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "large"})
	before, err := f.agents.GetDefinition(ctx, f.agent)
	require.NoError(t, err)
	assert.Nil(t, before.Cutover, "control: no attempt, no stage")

	f.switched(t, "import-1")
	after, err := f.agents.GetDefinition(ctx, f.agent)
	require.NoError(t, err)
	require.NotNil(t, after.Cutover)
	assert.Equal(t, agent.CutoverSwitched, *after.Cutover)
	assert.Equal(t, before.Values(), after.Values())
	assert.Equal(t, before.Operator(), after.Operator())
	listed, err := f.agents.ListDefinitionsByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.NotNil(t, listed[0].Cutover)
	assert.Equal(t, agent.CutoverSwitched, *listed[0].Cutover)

	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, valid("1")),
		"switched and not yet activated, the agent's own writes are admitted as before")
	assert.False(t, f.listed(t), "and no operator polls its definition")
}
