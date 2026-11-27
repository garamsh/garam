package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	"github.com/garamsh/garam/platform/internal/bot"
	botpg "github.com/garamsh/garam/platform/internal/bot/postgres"
)

// clusterReport is a report observed at at, carrying one cluster-owned
// object of kind named name in team-a.
func clusterReport(at time.Time, kind, name string) bot.Report {
	return bot.Report{
		ObservedAt:   at,
		Connector:    bot.ReportConnector{Version: "0.1.0", Mode: "apply", Namespaces: []string{"team-a"}},
		Declarations: []bot.DeclarationOutcome{},
		Objects: []bot.ReportedObject{{APIVersion: "bot.garam.sh/v1alpha1", Kind: kind, Namespace: "team-a",
			Name: name, UID: "u-" + name, Owner: bot.OwnerCluster, Generation: 1,
			Spec: json.RawMessage(`{"persona":{"role":"Lead"}}`), Status: json.RawMessage(`{}`)}},
	}
}

func objectNames(t *testing.T, s *botpg.Store, g grn.GRN) []string {
	t.Helper()
	kept, err := s.LatestReport(context.Background(), g)
	require.NoError(t, err)
	require.NotNil(t, kept.Report)
	var out []string
	for _, o := range kept.Report.Objects {
		out = append(out, o.Name)
	}
	return out
}

// TestBotReports_TheReportObservedLatestIsKept holds REP-5 at the
// store: a report replaces the kept one only where it was observed
// later, and a record with none reads as none.
func TestBotReports_TheReportObservedLatestIsKept(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]
	kept, err := s.LatestReport(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, bot.KeptReport{Instance: g}, kept, "a record that reported nothing")

	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.KeepReport(ctx, g, clusterReport(at, "Bot", "first")))
	require.NoError(t, s.KeepReport(ctx, g, clusterReport(at.Add(-time.Second), "Bot", "older")))
	require.NoError(t, s.KeepReport(ctx, g, clusterReport(at, "Bot", "same-instant")))
	assert.Equal(t, []string{"first"}, objectNames(t, s, g), "an older report, and one of the same instant, are ignored")
	require.NoError(t, s.KeepReport(ctx, g, clusterReport(at.Add(time.Second), "Bot", "newer")))
	assert.Equal(t, []string{"newer"}, objectNames(t, s, g), "the control: a later report replaces it")
	kept, err = s.LatestReport(ctx, g)
	require.NoError(t, err)
	assert.JSONEq(t, `{"persona":{"role":"Lead"}}`, string(kept.Report.Objects[0].Spec), "spec is kept as sent")

	gone := grn.New("acme", app.InstanceType, "0000000000000000")
	assert.ErrorIs(t, s.KeepReport(ctx, gone, clusterReport(at, "Bot", "x")), app.ErrInstanceNotFound)
	_, err = s.LatestReport(ctx, gone)
	assert.ErrorIs(t, err, app.ErrInstanceNotFound)
}

// TestBotReports_AnIdentifierIsPlacedOnTheRecordHoldingIt holds
// REP-8's lookup: a live and a tombstoned declaration of the record are
// placed on it, and another record's are not.
func TestBotReports_AnIdentifierIsPlacedOnTheRecordHoldingIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	records := declarationRecords(t, p, 2)
	g, other := records[0], records[1]
	for _, d := range []struct {
		on       grn.GRN
		id, name string
	}{{g, "live", "a"}, {g, "gone", "b"}, {other, "theirs", "c"}} {
		_, _, err := s.Declare(ctx, d.on, d.id, object(t, d.name))
		require.NoError(t, err)
	}
	_, err := s.Tombstone(ctx, g, "gone")
	require.NoError(t, err)

	placed, err := s.PlacedDeclarationIDs(ctx, g, []string{"live", "gone", "theirs", "never"})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"live": true, "gone": true}, placed)
}

// TestInstanceContact_ATokenThatResolvesRecordsTheContact holds REP-5's
// lastContactAt, which the app system records: resolving a token
// records the time, a second resolve within a second writes nothing,
// and one past it moves the time.
func TestInstanceContact_ATokenThatResolvesRecordsTheContact(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	keys := connectorKeys(t)
	g := registeredCluster(t, p, s, keys[0])
	token := &app.Token{Instance: g, Value: "garam_ct_contact"}
	require.NoError(t, s.RecordAssertionToken(ctx, signed(t, g, liveKey(t, s, g, keys[0]).ID, "jti-contact"), token))
	contact := func() *time.Time {
		at, err := s.LastContact(ctx, g)
		require.NoError(t, err)
		return at
	}
	assert.Nil(t, contact(), "a record that sent nothing")

	_, err := s.ResolveToken(ctx, app.HashSecret(token.Value))
	require.NoError(t, err)
	first := contact()
	require.NotNil(t, first)
	assert.WithinDuration(t, time.Now(), *first, time.Minute)
	_, err = s.ResolveToken(ctx, app.HashSecret(token.Value))
	require.NoError(t, err)
	assert.Equal(t, *first, *contact(), "a second request within a second writes nothing")

	_, err = p.Exec(ctx, `UPDATE instance_contacts SET last_contact_at = last_contact_at - interval '2 seconds'
		WHERE instance_grn = $1`, g.String())
	require.NoError(t, err)
	_, err = s.ResolveToken(ctx, app.HashSecret(token.Value))
	require.NoError(t, err)
	assert.True(t, contact().After(first.Add(-time.Second)), "a request past the window moves the time")

	_, err = s.ResolveToken(ctx, app.HashSecret("garam_ct_unknown"))
	assert.ErrorIs(t, err, app.ErrTokenInvalid, "a token that does not resolve records nothing")
}

// TestBotReports_ADeclarationOnANameTheReportShowsClusterOwnedIsRefused
// holds gbo ADR 0089 decision 4's courtesy: a declaration placed under
// a new identifier, one placed under an identifier chosen, and a next
// generation moved onto the name are each refused while the latest
// report shows a cluster-owned object there. Another name, another
// kind and a later report without the object are the controls.
func TestBotReports_ADeclarationOnANameTheReportShowsClusterOwnedIsRefused(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.KeepReport(ctx, g, clusterReport(at, "Bot", "mira")))

	drawn, err := bot.NewDeclarationID()
	require.NoError(t, err)
	_, _, err = s.Declare(ctx, g, drawn, object(t, "mira"))
	assert.ErrorIs(t, err, bot.ErrDeclaredObjectHeld, "a new declaration under a drawn identifier")
	_, _, err = s.Declare(ctx, g, "chosen", object(t, "mira"))
	assert.ErrorIs(t, err, bot.ErrDeclaredObjectHeld, "a new declaration under a chosen identifier")
	_, _, err = s.Declare(ctx, g, "moving", object(t, "reviewer"))
	require.NoError(t, err, "the control: a name the report does not show")
	_, _, err = s.Declare(ctx, g, "moving", object(t, "mira"))
	assert.ErrorIs(t, err, bot.ErrDeclaredObjectHeld, "a next generation moved onto the name")
	d, err := s.GetDeclaration(ctx, g, "moving")
	require.NoError(t, err)
	assert.Equal(t, int64(1), d.Generation, "the refused generation wrote nothing")

	require.NoError(t, s.KeepReport(ctx, g, clusterReport(at.Add(time.Second), "Workspace", "mira")))
	_, _, err = s.Declare(ctx, g, "chosen", object(t, "mira"))
	assert.NoError(t, err, "the control: the latest report shows another kind under the name")
}
