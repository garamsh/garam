package integration_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// claimedDefinition defines an agent operatorGRN runs and claims it,
// which is how an agent a definition describes comes to be held.
func claimedDefinition(t *testing.T, s *agent.PostgresStore, d *agent.Definition, operatorGRN grn.GRN) *agent.Assignment {
	t.Helper()
	assignment, err := s.Claim(context.Background(), d.Agent(), operatorGRN)
	require.NoError(t, err)
	return assignment
}

// applied is an application ignoring keys, which the test cannot
// build wrong.
func applied(t *testing.T, keys ...string) agent.ConfigurationApplied {
	t.Helper()
	a, err := agent.NewConfigurationApplied(keys)
	require.NoError(t, err)
	return a
}

// rejected is a rejection for reason.
func rejected(t *testing.T, reason string) agent.ConfigurationRejected {
	t.Helper()
	r, err := agent.ParseRejectionReason(reason)
	require.NoError(t, err)
	return agent.ConfigurationRejected{Reason: r}
}

// TestAgentDefinition_ASaveIsARevisionOfTheOneTheEditorRead is the lost
// update ADR-0076 closes, on the table. A definition starts at revision
// 1, saved when and by whom it was defined. A save against the revision
// it holds lands as the next one and moves only the values; the same
// save against a revision already moved past is refused and leaves the
// stored one standing. The control is that save carrying the current
// revision.
func TestAgentDefinition_ASaveIsARevisionOfTheOneTheEditorRead(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	d := defineAgent(t, p, s, operatorGRN, agent.Values{"model": "haiku"})
	claim := claimedDefinition(t, s, d, operatorGRN)

	first, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(1), first.Revision)
	assert.WithinDuration(t, d.CreatedAt(), first.RevisedAt, time.Microsecond)
	assert.Equal(t, d.DefinedBy(), first.RevisedBy)

	editor := scopedGRN("acme", "user", "grace")
	second, err := s.ReviseDefinition(ctx, d.Agent(), 1, agent.Values{"model": "sonnet"}, editor)
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(2), second.Revision)
	assert.Equal(t, agent.Values{"model": "sonnet"}, second.Values())
	assert.Equal(t, editor, second.RevisedBy)
	assert.True(t, second.RevisedAt.After(first.RevisedAt))
	// Only the values moved: the identity, the operator, who defined
	// the agent and when, and the claim's assignment are as they were.
	assert.Equal(t, d.Agent(), second.Agent())
	assert.Equal(t, d.Operator(), second.Operator())
	assert.Equal(t, d.DefinedBy(), second.DefinedBy())
	assert.WithinDuration(t, d.CreatedAt(), second.CreatedAt(), time.Microsecond)
	held, err := s.GetAssignment(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, claim.Epoch(), held.Epoch())
	assert.Equal(t, operatorGRN, held.Operator())

	_, err = s.ReviseDefinition(ctx, d.Agent(), 1, agent.Values{"model": "opus"}, editor)
	assert.ErrorIs(t, err, agent.ErrRevisionConflict)
	stored, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(2), stored.Revision)
	assert.Equal(t, agent.Values{"model": "sonnet"}, stored.Values(), "the stale save wrote over the saved one")

	third, err := s.ReviseDefinition(ctx, d.Agent(), 2, agent.Values{"model": "opus"}, editor)
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(3), third.Revision)

	_, err = s.ReviseDefinition(ctx, scopedGRN("acme", agent.ServiceType, "0f0f0f0f0f0f0f0f"), 1, agent.Values{}, editor)
	assert.ErrorIs(t, err, agent.ErrDefinitionNotFound)
}

// TestAgentDefinition_OfTwoConcurrentSavesOneLands races two saves read
// at one revision. Exactly one lands and the other is refused, so no
// interleaving lets both through as the same revision.
func TestAgentDefinition_OfTwoConcurrentSavesOneLands(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	d := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), agent.Values{})

	const rounds = 20
	for round := 1; round <= rounds; round++ {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = s.ReviseDefinition(ctx, d.Agent(), agent.Revision(round),
					agent.Values{"editor": strconv.Itoa(i)}, scopedGRN("acme", "user", "ada"))
			}()
		}
		wg.Wait()
		landed := 0
		for _, err := range errs {
			if err == nil {
				landed++
				continue
			}
			assert.ErrorIs(t, err, agent.ErrRevisionConflict)
		}
		assert.Equal(t, 1, landed, "round %d", round)
	}
	stored, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(rounds+1), stored.Revision)
}

// TestAgentConfiguration_WhatAnAcknowledgementMayMove walks every
// report ADR-0076 gives a meaning to, in order, against the table.
// Each refusal is read back on the row, beside the report that lands
// where it does not.
func TestAgentConfiguration_WhatAnAcknowledgementMayMove(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	d := defineAgent(t, p, s, operatorGRN, agent.Values{"model": "haiku"})
	epoch := claimedDefinition(t, s, d, operatorGRN).Epoch()
	editor := scopedGRN("acme", "user", "ada")

	// Claimed and never reported on: neither the health nor the
	// configuration has a time, and creation is not a report.
	fresh, err := s.Get(ctx, d.Agent())
	require.NoError(t, err)
	assert.Nil(t, fresh.HealthReportedAt, "creation was recorded as a health report")
	assert.Nil(t, fresh.Configuration)

	// A ready pod is a health report and says nothing of the revision
	// it runs, which is what an operator reporting no revision leaves.
	ready, err := s.ReportProvisioningState(ctx, d.Agent(), epoch, agent.ProvisioningStateReady)
	require.NoError(t, err)
	require.NotNil(t, ready.HealthReportedAt)
	assert.Nil(t, ready.Configuration, "a health report acknowledged a revision")

	// A revision the definition has not reached was never served.
	_, err = s.ReportConfiguration(ctx, d.Agent(), epoch, 2, applied(t))
	assert.ErrorIs(t, err, agent.ErrRevisionAhead)
	unmoved, err := s.Get(ctx, d.Agent())
	require.NoError(t, err)
	assert.Nil(t, unmoved.Configuration, "a future revision was recorded")

	// The revision it holds is acknowledged, at the epoch the report
	// carried.
	one, err := s.ReportConfiguration(ctx, d.Agent(), epoch, 1, applied(t, "model"))
	require.NoError(t, err)
	require.NotNil(t, one.Configuration)
	assert.Equal(t, epoch, one.Configuration.Epoch)
	assert.Equal(t, agent.Revision(1), one.Configuration.Revision)
	assert.Equal(t, applied(t, "model"), one.Configuration.Outcome)
	assert.False(t, one.Configuration.ReportedAt.IsZero())
	require.NotNil(t, one.HealthReportedAt)
	assert.True(t, ready.HealthReportedAt.Equal(*one.HealthReportedAt), "an acknowledgement moved the health time")

	// Two saves later, the operator acknowledges the middle one: it is
	// progress past what it acknowledged and short of what is saved,
	// and it is recorded.
	_, err = s.ReviseDefinition(ctx, d.Agent(), 1, agent.Values{"model": "sonnet"}, editor)
	require.NoError(t, err)
	_, err = s.ReviseDefinition(ctx, d.Agent(), 2, agent.Values{"model": "opus"}, editor)
	require.NoError(t, err)
	two, err := s.ReportConfiguration(ctx, d.Agent(), epoch, 2, applied(t))
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(2), two.Configuration.Revision)

	// An older revision would write over the newer acknowledgement, so
	// it is refused and the row keeps both the revision and its time.
	_, err = s.ReportConfiguration(ctx, d.Agent(), epoch, 1, applied(t))
	assert.ErrorIs(t, err, agent.ErrRevisionSuperseded)
	kept, err := s.Get(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(2), kept.Configuration.Revision)
	assert.True(t, two.Configuration.ReportedAt.Equal(kept.Configuration.ReportedAt), "a refused report moved the time")

	// The same revision again replaces the report in the order garam
	// receives them, outcome included, and moves the time received.
	againRejected, err := s.ReportConfiguration(ctx, d.Agent(), epoch, 2, rejected(t, "image pull failed"))
	require.NoError(t, err)
	assert.Equal(t, rejected(t, "image pull failed"), againRejected.Configuration.Outcome)
	assert.False(t, againRejected.Configuration.ReportedAt.Before(kept.Configuration.ReportedAt))
	againApplied, err := s.ReportConfiguration(ctx, d.Agent(), epoch, 2, applied(t, "workspace"))
	require.NoError(t, err)
	assert.Equal(t, applied(t, "workspace"), againApplied.Configuration.Outcome)

	// The saved revision, acknowledged as rejected.
	three, err := s.ReportConfiguration(ctx, d.Agent(), epoch, 3, rejected(t, "model not offered"))
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(3), three.Configuration.Revision)

	// Reassigning the agent fences the operator it replaced, and what
	// that operator acknowledged stays tied to its own epoch, so a
	// reader comparing epochs sees it is not the current assignment's.
	reassigned, err := s.Assign(ctx, d.Agent(), scopedGRN("acme", agent.ServiceTypeOperator, "two"))
	require.NoError(t, err)
	_, err = s.ReportConfiguration(ctx, d.Agent(), epoch, 3, applied(t))
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)
	previous, err := s.Get(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, epoch, previous.Configuration.Epoch)
	assert.NotEqual(t, reassigned.Epoch(), previous.Configuration.Epoch)

	// The new operator's first acknowledgement is not bounded by the
	// replaced operator's: an older revision from it is recorded.
	fromSuccessor, err := s.ReportConfiguration(ctx, d.Agent(), reassigned.Epoch(), 1, applied(t))
	require.NoError(t, err)
	assert.Equal(t, reassigned.Epoch(), fromSuccessor.Configuration.Epoch)
	assert.Equal(t, agent.Revision(1), fromSuccessor.Configuration.Revision)
}

// TestAgentConfiguration_AnAgentNoDefinitionDescribesAcknowledgesNothing
// covers the agent `createAgent` mints: it is held at an epoch, so the
// health report lands, and there is no revision to acknowledge.
func TestAgentConfiguration_AnAgentNoDefinitionDescribesAcknowledgesNothing(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	acme := orgRootGRN("acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	s := agent.NewPostgresStore(p)
	g := seedAgent(t, s, acme)
	assigned, err := s.Assign(ctx, g, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	require.NoError(t, err)

	_, err = s.ReportProvisioningState(ctx, g, assigned.Epoch(), agent.ProvisioningStateReady)
	require.NoError(t, err)
	_, err = s.ReportConfiguration(ctx, g, assigned.Epoch(), 1, applied(t))
	assert.ErrorIs(t, err, agent.ErrDefinitionNotFound)
}

// TestAgentConfiguration_AnAcknowledgementRacingAReassignmentIsNeverCurrent
// races the replaced operator's acknowledgement against the
// reassignment that replaces it. Whichever lands first, the row never
// carries an acknowledgement at the epoch the agent is now assigned at:
// either the fence refused it, or it carries the epoch it was made at,
// which the reassignment has moved past.
func TestAgentConfiguration_AnAcknowledgementRacingAReassignmentIsNeverCurrent(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	d := defineAgent(t, p, s, operatorGRN, agent.Values{})
	epoch := claimedDefinition(t, s, d, operatorGRN).Epoch()

	acknowledgement := applied(t)
	for round := 0; round < 20; round++ {
		var wg sync.WaitGroup
		var reportErr error
		var reassigned *agent.Assignment
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, reportErr = s.ReportConfiguration(ctx, d.Agent(), epoch, 1, acknowledgement)
		}()
		go func() {
			defer wg.Done()
			var err error
			reassigned, err = s.Assign(ctx, d.Agent(), operatorGRN)
			assert.NoError(t, err)
		}()
		wg.Wait()
		require.NotNil(t, reassigned)
		require.Equal(t, epoch+1, reassigned.Epoch())
		if reportErr != nil {
			assert.ErrorIs(t, reportErr, agent.ErrStaleEpoch)
		}
		stored, err := s.Get(ctx, d.Agent())
		require.NoError(t, err)
		if stored.Configuration != nil {
			assert.NotEqual(t, reassigned.Epoch(), stored.Configuration.Epoch,
				"round %d: the replaced operator's acknowledgement reads as current", round)
		}
		epoch = reassigned.Epoch()
	}
}

// TestAgentConfiguration_TheSchemaKeepsAnAcknowledgementWhole writes
// the columns around the domain. Each partial or contradictory row is
// refused beside the whole one that differs from it in that column.
func TestAgentConfiguration_TheSchemaKeepsAnAcknowledgementWhole(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	acme := orgRootGRN("acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	g := seedAgent(t, agent.NewPostgresStore(p), acme)

	write := func(set string) error {
		_, err := p.Exec(ctx, `UPDATE agents SET `+set+` WHERE grn = $1`, g.String())
		return err
	}
	const whole = `configuration_epoch = 1, configuration_revision = 1, configuration_reported_at = now(), `
	cases := map[string]struct{ refused, accepted string }{
		"an acknowledgement with no time": {
			`configuration_epoch = 1, configuration_revision = 1, configuration_outcome = 'applied', configuration_ignored_keys = '{}'`,
			whole + `configuration_outcome = 'applied', configuration_ignored_keys = '{}'`},
		"an application with no ignored keys": {
			whole + `configuration_outcome = 'applied', configuration_ignored_keys = NULL`,
			whole + `configuration_outcome = 'applied', configuration_ignored_keys = '{}'`},
		"an application with a reason": {
			whole + `configuration_outcome = 'applied', configuration_ignored_keys = '{}', configuration_reason = 'x'`,
			whole + `configuration_outcome = 'applied', configuration_ignored_keys = '{}', configuration_reason = NULL`},
		"a rejection with a blank reason": {
			whole + `configuration_outcome = 'rejected', configuration_ignored_keys = NULL, configuration_reason = ' '`,
			whole + `configuration_outcome = 'rejected', configuration_ignored_keys = NULL, configuration_reason = 'x'`},
		"a rejection with a reason past 500 characters": {
			whole + `configuration_outcome = 'rejected', configuration_ignored_keys = NULL, configuration_reason = repeat('가', 501)`,
			whole + `configuration_outcome = 'rejected', configuration_ignored_keys = NULL, configuration_reason = repeat('가', 500)`},
		"an outcome no one names": {
			whole + `configuration_outcome = 'partial', configuration_ignored_keys = '{}'`,
			whole + `configuration_outcome = 'applied', configuration_ignored_keys = '{}'`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, write(c.refused))
			assert.NoError(t, write(c.accepted))
			require.NoError(t, write(`configuration_epoch = NULL, configuration_revision = NULL,
				configuration_outcome = NULL, configuration_reason = NULL,
				configuration_ignored_keys = NULL, configuration_reported_at = NULL`))
		})
	}
}

// TestConfigurationSurface_AnOperatorAcknowledgesWhatItRead carries the
// route over the mTLS listener against real Postgres: the operator
// reads the revision on `listDefinitions`, a report without one leaves
// application unknown, and its acknowledgement lands or is refused as
// the store decides.
func TestConfigurationSurface_AnOperatorAcknowledgesWhatItRead(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)

	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	client := machineClient(t, installed(t, p, outOfBand, operatorGRN))
	store := agent.NewPostgresStore(p)
	d := defineAgent(t, p, store, operatorGRN, agent.Values{"model": "haiku"})
	status, body := machineRequest(t, client, http.MethodPost, url+"/definitions/"+d.Agent().String()+"/claim")
	require.Equal(t, http.StatusCreated, status, string(body))

	read := definitionsOf(t, client, url)
	require.Len(t, read, 1)
	assert.Equal(t, 1, read[0].Revision)

	route := url + "/agents/" + d.Agent().String()
	status, body = machineSend(t, client, http.MethodPost, route+"/provisioning-state", `{"epoch":1,"state":"ready"}`)
	require.Equal(t, http.StatusNoContent, status, string(body))
	legacy, err := store.Get(ctx, d.Agent())
	require.NoError(t, err)
	assert.Nil(t, legacy.Configuration, "a readiness report acknowledged a revision")

	status, body = machineSend(t, client, http.MethodPost, route+"/configuration-state",
		`{"epoch":1,"revision":2,"outcome":"applied","ignoredKeys":[]}`)
	assert.Equal(t, http.StatusConflict, status, string(body))
	status, body = machineSend(t, client, http.MethodPost, route+"/configuration-state",
		`{"epoch":1,"revision":1,"outcome":"applied","ignoredKeys":["model"]}`)
	require.Equal(t, http.StatusNoContent, status, string(body))
	acknowledged, err := store.Get(ctx, d.Agent())
	require.NoError(t, err)
	require.NotNil(t, acknowledged.Configuration)
	assert.Equal(t, applied(t, "model"), acknowledged.Configuration.Outcome)
}

// TestMigration27_GivesEveryExistingDefinitionItsFirstRevision migrates
// a schema of its own to the version before 000027, writes a
// definition and an agent reported on there as rows written then were,
// and applies 000027. The definition holds revision 1, saved when and
// by whom it was defined; the agent has no health report time and no
// acknowledgement, because none was ever recorded. The second
// definition is the control for the backfill reading each row's own
// columns.
func TestMigration27_GivesEveryExistingDefinitionItsFirstRevision(t *testing.T) {
	const schema = "migration_27_test"
	p := pool(t)
	databaseURL := isolatedSchema(t, p, schema)
	dir := t.TempDir()

	entries, err := os.ReadDir(migrationsDir(t))
	require.NoError(t, err)
	var later []string
	before := 0
	for _, e := range entries {
		version, _, ok := strings.Cut(e.Name(), "_")
		n, err := strconv.Atoi(version)
		if !ok || err != nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if n >= 27 {
			later = append(later, e.Name())
			continue
		}
		before = max(before, n)
		copyMigration(t, dir, e.Name())
	}
	require.Contains(t, later, "000027_agent_configuration.up.sql")
	require.NoError(t, runMigrate(t, dir, databaseURL))
	version, _ := schemaVersion(t, p, schema)
	require.Equal(t, before, version)

	ctx := context.Background()
	exec := func(query string) {
		t.Helper()
		_, err := p.Exec(ctx, fmt.Sprintf(query, schema))
		require.NoError(t, err)
	}
	exec(`INSERT INTO %s.orgs (grn, name, created_at, updated_at)
		VALUES ('grn:root:default:org:acme', 'acme', now(), now())`)
	exec(`INSERT INTO %s.operators (grn, org_grn, registered_by, created_at, name)
		VALUES ('grn:acme:default:operator:one', 'grn:root:default:org:acme', 'grn:acme:default:user:ada', now(), 'one')`)
	exec(`INSERT INTO %s.agent_definitions (agent_grn, org_grn, operator_grn, name, agent_values, defined_by, created_at) VALUES
		('grn:acme:default:agent:0a0a0a0a0a0a0a0a', 'grn:root:default:org:acme', 'grn:acme:default:operator:one', 'first', '{}', 'grn:acme:default:user:ada', '2026-09-01T00:00:00Z'),
		('grn:acme:default:agent:0b0b0b0b0b0b0b0b', 'grn:root:default:org:acme', 'grn:acme:default:operator:one', 'second', '{}', 'grn:acme:default:user:grace', '2026-09-02T00:00:00Z')`)
	exec(`INSERT INTO %s.agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
		VALUES ('grn:acme:default:agent:0a0a0a0a0a0a0a0a', 'grn:root:default:org:acme', 'ready', 'connected',
		        '2026-09-01T00:00:00Z', '2026-09-03T00:00:00Z')`)

	for _, name := range later {
		copyMigration(t, dir, name)
	}
	require.NoError(t, runMigrate(t, dir, databaseURL))

	rows, err := p.Query(ctx, fmt.Sprintf(`SELECT agent_grn, revision, revised_at = created_at, revised_by
		FROM %s.agent_definitions ORDER BY agent_grn`, schema))
	require.NoError(t, err)
	type revised struct {
		agent     string
		revision  int
		atCreated bool
		by        string
	}
	var got []revised
	for rows.Next() {
		var r revised
		require.NoError(t, rows.Scan(&r.agent, &r.revision, &r.atCreated, &r.by))
		got = append(got, r)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	assert.Equal(t, []revised{
		{"grn:acme:default:agent:0a0a0a0a0a0a0a0a", 1, true, "grn:acme:default:user:ada"},
		{"grn:acme:default:agent:0b0b0b0b0b0b0b0b", 1, true, "grn:acme:default:user:grace"},
	}, got)

	var healthUnknown, configurationUnknown bool
	require.NoError(t, p.QueryRow(ctx, fmt.Sprintf(`SELECT health_reported_at IS NULL, configuration_epoch IS NULL
		FROM %s.agents`, schema)).Scan(&healthUnknown, &configurationUnknown))
	assert.True(t, healthUnknown, "a report time was invented from updated_at")
	assert.True(t, configurationUnknown, "an acknowledgement was invented")
}
