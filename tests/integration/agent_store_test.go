package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// TestAgentStore_RoundTrip drives an agent through the shape ADR-0012
// decided: garam mints its GRN, an operator is assigned to it, a
// reassignment raises the epoch, and the operator that was replaced
// writes nothing afterwards.
func TestAgentStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))

	got, err := s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, orgGRN, got.Organization())
	assert.Equal(t, agent.ProvisioningStatePending, got.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, got.RuntimeState())
	// The stamps read back are the ones New set, to the microsecond
	// Postgres keeps them at.
	assert.WithinDuration(t, a.CreatedAt(), got.CreatedAt(), time.Microsecond)
	assert.WithinDuration(t, a.UpdatedAt(), got.UpdatedAt(), time.Microsecond)

	_, err = s.Get(ctx, grnOf("agent", "missing"))
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)

	// Assignment is a relation, so an unassigned agent is a stored
	// agent with no operator.
	_, err = s.GetAssignment(ctx, g)
	assert.ErrorIs(t, err, agent.ErrAssignmentNotFound)

	// An agent with no assignment has no current epoch, so every report
	// about it is stale.
	_, err = s.ReportProvisioningState(ctx, g, 1, agent.ProvisioningStateReady)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)
	_, err = s.ReportRuntimeState(ctx, g, 1, agent.RuntimeStateConnected, legacyFence(g))
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	first := grnOf(agent.ServiceTypeOperator, "one")
	assigned, err := s.Assign(ctx, g, first)
	require.NoError(t, err)
	assert.Equal(t, first, assigned.Assignee())
	assert.Equal(t, 1, assigned.Epoch())
	assert.True(t, assigned.CreatedAt().After(a.CreatedAt()), "the assignment is stamped when it is written, after the agent it binds")

	// The operator reports the pod; the agent reports itself. Neither
	// report moves the other's state.
	provisioned, err := s.ReportProvisioningState(ctx, g, assigned.Epoch(), agent.ProvisioningStateReady)
	require.NoError(t, err)
	assert.Equal(t, agent.ProvisioningStateReady, provisioned.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, provisioned.RuntimeState())
	// A report moves the update stamp and leaves the create stamp, so
	// each is read from its own column.
	assert.WithinDuration(t, a.CreatedAt(), provisioned.CreatedAt(), time.Microsecond)
	assert.True(t, provisioned.UpdatedAt().After(a.UpdatedAt()), "a report moves the update stamp")

	running, err := s.ReportRuntimeState(ctx, g, assigned.Epoch(), agent.RuntimeStateConnected, legacyFence(g))
	require.NoError(t, err)
	assert.Equal(t, agent.RuntimeStateConnected, running.RuntimeState())
	assert.Equal(t, agent.ProvisioningStateReady, running.ProvisioningState())

	second := grnOf(agent.ServiceTypeOperator, "two")
	reassigned, err := s.Assign(ctx, g, second)
	require.NoError(t, err)
	assert.Equal(t, second, reassigned.Assignee())
	assert.Equal(t, 2, reassigned.Epoch(), "a reassignment raises the epoch")
	// A reassignment writes onto the row the first one left, so the
	// create stamp is still the first one's.
	assert.Equal(t, assigned.CreatedAt(), reassigned.CreatedAt())
	assert.True(t, reassigned.UpdatedAt().After(assigned.UpdatedAt()), "and moves the update stamp")

	stored, err := s.GetAssignment(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, second, stored.Assignee())
	assert.Equal(t, 2, stored.Epoch())

	// A pod left running by the replaced operator still holds epoch 1.
	_, err = s.ReportProvisioningState(ctx, g, assigned.Epoch(), agent.ProvisioningStateFailed)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)
	_, err = s.ReportRuntimeState(ctx, g, assigned.Epoch(), agent.RuntimeStateDisconnected, legacyFence(g))
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	// An epoch too wide for the column shares its low 32 bits with the
	// live one and is stale all the same.
	_, err = s.ReportRuntimeState(ctx, g, stored.Epoch()+1<<32, agent.RuntimeStateDisconnected, legacyFence(g))
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	after, err := s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, agent.ProvisioningStateReady, after.ProvisioningState(), "the fenced write left the row alone")
	assert.Equal(t, agent.RuntimeStateConnected, after.RuntimeState())

	// The current epoch still writes.
	current, err := s.ReportRuntimeState(ctx, g, stored.Epoch(), agent.RuntimeStateDisconnected, legacyFence(g))
	require.NoError(t, err)
	assert.Equal(t, agent.RuntimeStateDisconnected, current.RuntimeState())

	_, err = s.ReportRuntimeState(ctx, grnOf("agent", "missing"), 1, agent.RuntimeStateConnected, legacyFence(grnOf("agent", "missing")))
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)

	// An agent belongs to its org and disappears with it, assignment
	// included.
	require.NoError(t, orgpg.New(p).DeleteOrg(ctx, orgGRN))
	_, err = s.Get(ctx, g)
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)
	var assignments int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM agent_assignments`).Scan(&assignments))
	assert.Zero(t, assignments)
}

// TestAgentStore_ListsTheOrgsOwnAgents covers what the browser read
// path is served from: an organization's agents and the assignments
// holding them, each ordered and each scoped to that organization.
// Every state moves before the read, so a list that answered from the
// wrong column or the wrong row is visible.
func TestAgentStore_ListsTheOrgsOwnAgents(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgs := orgpg.New(p)
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	held := seedAgent(t, s, acme)
	loose := seedAgent(t, s, acme)
	elsewhere := seedAgent(t, s, globex)

	operator := scopedGRN(acme.Identifier, agent.ServiceTypeOperator, "field-one")
	assigned, err := s.Assign(ctx, held, operator)
	require.NoError(t, err)
	_, err = s.ReportProvisioningState(ctx, held, assigned.Epoch(), agent.ProvisioningStateReady)
	require.NoError(t, err)
	_, err = s.ReportRuntimeState(ctx, held, assigned.Epoch(), agent.RuntimeStateConnected, legacyFence(held))
	require.NoError(t, err)
	_, err = s.Assign(ctx, elsewhere, scopedGRN(globex.Identifier, agent.ServiceTypeOperator, "field-two"))
	require.NoError(t, err)

	listed, err := s.ListByOrg(ctx, acme, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, listed, 2, "the org's own agents and no other org's")
	assert.ElementsMatch(t, []grn.GRN{held, loose}, []grn.GRN{listed[0].GRN(), listed[1].GRN()})
	assert.Less(t, listed[0].GRN().String(), listed[1].GRN().String(), "ordered by GRN")

	page, err := s.ListByOrg(ctx, acme, listfilter.Filter{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{listed[0].GRN()}, []grn.GRN{page[0].GRN()}, "the limit takes the first page and stops")
	require.Len(t, page, 1)

	page, err = s.ListByOrg(ctx, acme, listfilter.Filter{Offset: 1})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, listed[1].GRN(), page[0].GRN(), "the offset skips the first agent")

	byGRN := map[grn.GRN]*agent.Agent{}
	for _, a := range listed {
		byGRN[a.GRN()] = a
	}
	// Each reported state comes back in its own field, and the agent
	// nobody reported on keeps the initial pair.
	assert.Equal(t, agent.ProvisioningStateReady, byGRN[held].ProvisioningState())
	assert.Equal(t, agent.RuntimeStateConnected, byGRN[held].RuntimeState())
	assert.Equal(t, agent.ProvisioningStatePending, byGRN[loose].ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, byGRN[loose].RuntimeState())

	// An unassigned agent contributes no assignment, so the list is not
	// one per agent.
	assignments, err := s.ListAssignmentsByOrg(ctx, acme)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	assert.Equal(t, held, assignments[0].Agent())
	assert.Equal(t, operator, assignments[0].Assignee())
	assert.Equal(t, 1, assignments[0].Epoch())

	// The other org holds its own agent and its own assignment, so the
	// scoping refuses a pairing rather than every pairing.
	listed, err = s.ListByOrg(ctx, globex, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, elsewhere, listed[0].GRN())
	assignments, err = s.ListAssignmentsByOrg(ctx, globex)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	assert.Equal(t, elsewhere, assignments[0].Agent())

	// An org that holds none reads as empty rather than as everyone's.
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("initech"), "initech")))
	listed, err = s.ListByOrg(ctx, orgRootGRN("initech"), listfilter.Filter{})
	require.NoError(t, err)
	assert.Empty(t, listed)
	assignments, err = s.ListAssignmentsByOrg(ctx, orgRootGRN("initech"))
	require.NoError(t, err)
	assert.Empty(t, assignments)
}

// TestAgent_GRNNamesAnAgentOfItsOrg covers the schema's copy of the
// ownership rule the agent domain enforces: a row whose GRN names
// another organization, or names something that is not an agent, is
// refused even by a statement that never passes through the domain. A
// row the database accepted and the domain refused to read would be
// neither writable nor readable.
func TestAgent_GRNNamesAnAgentOfItsOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))

	// agent.New refuses both pairings, so only a direct statement reaches
	// the constraint. Both cases name the same rule — the GRN names an
	// agent, and it names one of this row's org — and naming it is what
	// says that rule refused the row rather than a column check or the
	// org foreign key.
	rejected := []struct {
		name string
		grn  grn.GRN
	}{
		{"another org's agent", scopedGRN("globex", agent.ServiceType, "deadbeef")},
		{"an identity that is not an agent", grnOf("user", "ada")},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Exec(ctx, `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
				VALUES ($1, $2, 'pending', 'unknown', now(), now())`, tt.grn.String(), orgGRN.String())
			var refused *pgconn.PgError
			require.ErrorAs(t, err, &refused, "the row was not refused by Postgres")
			assert.Equal(t, "agents_grn_ownership", refused.ConstraintName)
		})
	}
}

// TestAgentAssignment_StaysInsideItsOrg covers the schema's rules for an
// assignment (ADR-0015): the GRN it carries names an operator, and that
// operator belongs to the agent's org. An operator is addressed as an
// agent is (ADR-0012), so a root-owned one has no place here and is not
// admitted the way a policy attachment admits a root-owned subject.
func TestAgentAssignment_StaysInsideItsOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))

	rejected := []struct {
		name     string
		operator grn.GRN
	}{
		{"another org's operator", scopedGRN("globex", agent.ServiceTypeOperator, "one")},
		{"a root-owned operator", grn.New(grn.RootOwner, grn.DefaultProject, agent.ServiceTypeOperator, "one")},
		{"an identity that is not an operator", grnOf("user", "ada")},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Assign(ctx, g, tt.operator)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
			_, err = s.GetAssignment(ctx, g)
			assert.ErrorIs(t, err, agent.ErrAssignmentNotFound, "the rejected assignment was not stored")
		})
	}

	// The operator of the agent's own org is assigned, so the rules
	// reject a pairing rather than every pairing.
	operator := grnOf(agent.ServiceTypeOperator, "one")
	assigned, err := s.Assign(ctx, g, operator)
	require.NoError(t, err)
	assert.Equal(t, operator, assigned.Assignee())

	// A reassignment writes onto the row the agent already has, and it
	// leaves the org the same way an assignment does.
	_, err = s.Assign(ctx, g, scopedGRN("globex", agent.ServiceTypeOperator, "two"))
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	stored, err := s.GetAssignment(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, operator, stored.Assignee(), "the rejected reassignment left the row alone")
	assert.Equal(t, 1, stored.Epoch())
}

// TestAgentDefinitionStore_RoundTrip covers what a definition is
// (ADR-0026): an organization's record of what an agent should be,
// kept from before garam stopped accepting them, holding no agent.
// The agent GRN it names answers nothing among the agents, because
// garam learns an agent exists from the operator that reports it.
func TestAgentDefinitionStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgs := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	otherOrgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(otherOrgGRN, "globex")))

	values, err := agent.ParseValues(map[string]string{"model": "haiku", "workspace": "frontend"})
	require.NoError(t, err)
	definedBy := grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada")
	d, err := agent.NewDefinition(orgGRN, registerOperator(t, p, grnOf(agent.ServiceTypeOperator, "one")), "Field agent", values, definedBy)
	require.NoError(t, err)
	keepDefinition(t, p, d)

	stored, err := s.ListDefinitionsByOrg(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, d.Agent(), stored[0].Agent())
	assert.Equal(t, d.Operator(), stored[0].Operator())
	assert.Equal(t, definedBy, stored[0].DefinedBy())
	assert.Equal(t, values, stored[0].Values(), "every key and value survives the column")
	assert.False(t, stored[0].CreatedAt().IsZero())

	// The definition put no agent anywhere: reading the GRN it names
	// answers as it did before the definition was written, while an
	// agent an operator reported is found.
	_, err = s.Get(ctx, d.Agent())
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)
	_, err = s.GetAssignment(ctx, d.Agent())
	assert.ErrorIs(t, err, agent.ErrAssignmentNotFound)
	reported, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(reported, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))
	_, err = s.Get(ctx, reported)
	assert.NoError(t, err, "an agent that was reported is found, so the miss above is the definition")
	agents, err := s.ListByOrg(ctx, orgGRN, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, reported, agents[0].GRN())

	// A definition belongs to the org that wrote it.
	elsewhere, err := agent.NewDefinition(otherOrgGRN, registerOperator(t, p, scopedGRN("globex", agent.ServiceTypeOperator, "one")), "Field agent", agent.Values{}, definedBy)
	require.NoError(t, err)
	keepDefinition(t, p, elsewhere)
	stored, err = s.ListDefinitionsByOrg(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, stored, 1, "this org's definitions and no other org's")
	assert.Equal(t, d.Agent(), stored[0].Agent())

	// An empty payload is stored as one and read back as one: an
	// operator reads no values rather than a missing object.
	storedElsewhere, err := s.ListDefinitionsByOrg(ctx, otherOrgGRN)
	require.NoError(t, err)
	require.Len(t, storedElsewhere, 1)
	assert.Equal(t, agent.Values{}, storedElsewhere[0].Values())
}

// TestAgentDefinition_StaysInsideItsOrgAndHoldsAnObject covers the
// schema's rules for a definition. internal/agent refuses the same
// pairings, so only a direct statement reaches the constraints — and a
// row the database accepted and the domain refused to read would be
// neither writable nor readable.
func TestAgentDefinition_StaysInsideItsOrgAndHoldsAnObject(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	registered := registerOperator(t, p, grnOf(agent.ServiceTypeOperator, "one"))

	// A second organization with an operator of its own, so a case that
	// names one rule violates that rule alone. Reaching across the
	// boundary with this org's operator would break the ownership rule
	// and the same-org rule together, and dropping either would leave
	// the other still refusing the row.
	require.NoError(t, orgpg.New(p).CreateOrg(ctx,
		org.NewOrganization(orgRootGRN("globex"), "globex")))
	globexOperator := registerOperator(t, p, scopedGRN("globex", agent.ServiceTypeOperator, "one"))

	// A case naming an operator or a value rule takes an agent GRN of its
	// own. agent_grn is the primary key, so two cases sharing one collide
	// the moment a dropped rule lets the first row in — the sibling then
	// fails on the key rather than on the rule it names, and this test
	// stops isolating the thing it exists to isolate.
	acmeAgent := func() grn.GRN {
		minted, err := agent.NewGRN(orgGRN)
		require.NoError(t, err)
		return minted
	}

	// `constraint` is the rule Postgres must refuse the row by. Asserting
	// it is what separates these cases: five rules share one statement,
	// and a bare error cannot say which of them did the refusing, so any
	// one could stop being enforced while its siblings kept this green.
	rejected := []struct {
		name       string
		agentGRN   grn.GRN
		operator   grn.GRN
		values     string
		constraint string
	}{
		{"another org's agent", scopedGRN("globex", agent.ServiceType, "deadbeef"), globexOperator, `{}`,
			"agent_definitions_grn_ownership"},
		{"an identity that is not an agent", grnOf("user", "ada"), registered, `{}`,
			"agent_definitions_grn_ownership"},
		{"another org's operator", acmeAgent(), globexOperator, `{}`,
			"agent_definitions_same_org"},
		{"an identity that is not an operator", acmeAgent(), grnOf(agent.ServiceType, "deadbeef"), `{}`,
			"agent_definitions_operator_service_type"},
		{"an operator this org has not registered", acmeAgent(), grnOf(agent.ServiceTypeOperator, "two"), `{}`,
			"agent_definitions_operator_registered"},
		{"values that are not an object", acmeAgent(), registered, `[]`,
			"agent_definitions_values_object"},
	}
	// 000035 refuses every insert before a constraint is read
	// (ADR-0090), so each one is made with that guard off, as a release
	// before it made one, and rolled back.
	insert := func(agentGRN, operatorGRN grn.GRN, values string) error {
		tx, err := p.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, "ALTER TABLE agent_definitions DISABLE TRIGGER agent_definitions_closed")
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, name, agent_values, defined_by, created_at)
			VALUES ($1, $2, $3, 'Field agent', $4::jsonb, $5, now())`,
			agentGRN.String(), orgGRN.String(), operatorGRN.String(), values, grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada").String())
		return err
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			err := insert(tt.agentGRN, tt.operator, tt.values)
			var refused *pgconn.PgError
			require.ErrorAs(t, err, &refused, "the row was not refused by Postgres")
			assert.Equal(t, tt.constraint, refused.ConstraintName)
		})
	}

	// The same statement naming this org's agent and operator is
	// accepted, so the constraints reject a row rather than every row.
	require.NoError(t, insert(g, registered, `{"model":"haiku"}`))
}
