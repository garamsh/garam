package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// installationMark and instanceMark are the outbox keys of the
// entities.
func installationMark(g grn.GRN) string { return "Iam::Installation " + g.String() }
func instanceMark(g grn.GRN) string     { return "Iam::Instance " + g.String() }

// TestPolicyOutbox_AnInstallationAndItsInstancesMarkThemselves holds
// each write of an installation or an instance against the whole set
// of marks it leaves (ADR-0192 decision 9). Before every write the
// outbox holds a mark of an unrelated entity, which a write must leave
// alone, and another organization's installation and instance stand
// beside, which no write here may mark.
func TestPolicyOutbox_AnInstallationAndItsInstancesMarkThemselves(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	other := connectorInstallation(t, p, "globex")
	instancesOf(t, p, other, 1)
	startFeed(t, p)
	unrelated := "Iam::User grn:acme/iam/user/lee"

	var installation *app.Installation
	var first, second grn.GRN
	steps := []struct {
		name string
		do   func()
		want func() []string
	}{
		{"an app is installed, in an organization made for it",
			func() { installation = connectorInstallation(t, p, "acme") },
			func() []string {
				return []string{"Iam::Org grn:root/iam/org/acme", "Iam::OrgMembers grn:root/iam/org/acme",
					installationMark(installation.GRN)}
			}},
		{"an instance is created",
			func() { first = instancesOf(t, p, installation, 1)[0] },
			func() []string { return []string{instanceMark(first)} }},
		{"the instance's name, description and kind are written",
			func() {
				name := "renamed"
				patch, err := app.NewDetailsPatch(&name, &name, &name)
				require.NoError(t, err)
				_, err = s.UpdateInstance(ctx, first, patch)
				require.NoError(t, err)
			},
			func() []string { return nil }},
		{"an instance is deleted",
			func() { require.NoError(t, s.DeleteInstance(ctx, first)) },
			func() []string { return []string{instanceMark(first)} }},
		{"the installation is deleted, which takes its instance",
			func() {
				second = instancesOf(t, p, installation, 1)[0]
				execSQL(t, p, `DELETE FROM policy_outbox WHERE entity_type = 'Iam::Instance'`)
				removed, err := s.DeleteInstallation(ctx, installation.GRN)
				require.NoError(t, err)
				assert.Equal(t, []grn.GRN{second}, removed, "it answers the instance it took")
			},
			func() []string { return []string{installationMark(installation.GRN), instanceMark(second)} }},
	}
	for _, step := range steps {
		execSQL(t, p, `DELETE FROM policy_outbox`)
		execSQL(t, p, `SELECT policy_outbox_mark('Iam::User', 'grn:acme/iam/user/lee')`)

		step.do()

		want := append([]string{unrelated}, step.want()...)
		assert.ElementsMatch(t, want, markKeys(outboxMarks(t, p)), step.name)
	}

	// The organization's deletion takes its installation and the
	// instance in it, and marks both.
	theirs := instancesOf(t, p, other, 1)[0]
	execSQL(t, p, `DELETE FROM policy_outbox`)
	require.NoError(t, orgpg.New(p).DeleteOrg(ctx, orgRootGRN("globex")))
	assert.Subset(t, markKeys(outboxMarks(t, p)), []string{installationMark(other.GRN), instanceMark(theirs)})
}

// TestPolicyOutbox_AFeedThatIsOffMarksNoInstallation is the control
// for the marks above: the same writes with the feed off leave the
// outbox empty, and the feed's start marks every installation and
// instance that stands.
func TestPolicyOutbox_AFeedThatIsOffMarksNoInstallation(t *testing.T) {
	p := pool(t)
	require.False(t, feedEnabled(t, p))
	installation := connectorInstallation(t, p, "acme")
	instance := instancesOf(t, p, installation, 1)[0]
	assert.Empty(t, outboxMarks(t, p))

	startFeed(t, p)
	assert.Subset(t, markKeys(outboxMarks(t, p)), []string{installationMark(installation.GRN), instanceMark(instance)},
		"the feed's start marks what stood before it")
}

// TestPolicyEntityState_AnInstanceSitsInItsInstallation holds the
// state of each: an installation in its organization and among its
// members, as an agent is, and an instance in its installation alone,
// each with its organization; another organization's sit under their
// own. A record the tables do not hold has no state.
func TestPolicyEntityState_AnInstanceSitsInItsInstallation(t *testing.T) {
	p := pool(t)
	acme := connectorInstallation(t, p, "acme")
	globex := connectorInstallation(t, p, "globex")
	ours := instancesOf(t, p, acme, 1)[0]
	theirs := instancesOf(t, p, globex, 1)[0]

	for _, c := range []struct {
		installation *app.Installation
		instance     grn.GRN
	}{{acme, ours}, {globex, theirs}} {
		org := c.installation.Organization.String()
		state := readEntityState(t, p, "Iam::Installation", c.installation.GRN.String())
		require.NotNil(t, state)
		assert.Equal(t, entityState{org: ptr(org), parents: []string{"Iam::Org " + org, "Iam::OrgMembers " + org}}, *state)

		state = readEntityState(t, p, "Iam::Instance", c.instance.String())
		require.NotNil(t, state)
		assert.Equal(t, entityState{org: ptr(org), parents: []string{installationMark(c.installation.GRN)}}, *state)
	}

	assert.Nil(t, readEntityState(t, p, "Iam::Instance", "grn:acme/iam/instance/0000000000000000"))
	assert.Nil(t, readEntityState(t, p, "Iam::Installation", "grn:acme/iam/installation/absent"))
}
