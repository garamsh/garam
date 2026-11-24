package cluster_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/operator"
)

// report is a report REP-1 and REP-2 admit: an outcome and a
// garam-owned object of decl-a, and a cluster-owned object. Each case
// below changes one member of it.
const report = `{"observedAt":"2026-11-24T12:00:00Z",
	"connector":{"version":"0.1.0","mode":"apply","namespaces":["team-a"]},
	"declarations":[{"id":"decl-a","generation":1,"outcome":"applied"}],
	"objects":[
		{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","namespace":"team-a","name":"reviewer","uid":"u1",
		 "owner":"garam","generation":1,"cluster":"grn:acme/iam/cluster/5f60710a1b2c3d4e","declarationId":"decl-a",
		 "orphaned":false,"status":{}},
		{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","namespace":"team-a","name":"mira","uid":"u2",
		 "owner":"cluster","generation":3,"spec":{"persona":{"role":"Lead"}},"status":{"phase":"Ready"}}]}`

var reporter = grn.New("acme", cluster.ServiceType, "5f60710a1b2c3d4e")

func TestParseReport_RefusesWhatTheInterfaceRefuses(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"no observedAt", `"observedAt":"2026-11-24T12:00:00Z",`, ``},
		{"an unknown mode", `"mode":"apply"`, `"mode":"push"`},
		{"no namespaces", `,"namespaces":["team-a"]`, ``},
		{"no objects", `"objects":[`, `"other":[`},
		{"an unknown outcome", `"outcome":"applied"`, `"outcome":"done"`},
		{"an outcome with no generation", `"generation":1,"outcome"`, `"outcome"`},
		{"a Secret (REP-7)", `"kind":"Bot","namespace":"team-a","name":"mira"`, `"kind":"Secret","namespace":"team-a","name":"mira"`},
		{"a ConfigMap (REP-7)", `"kind":"Bot","namespace":"team-a","name":"mira"`, `"kind":"ConfigMap","namespace":"team-a","name":"mira"`},
		{"an undeclared kind (REP-7)", `"kind":"Bot","namespace":"team-a","name":"mira"`, `"kind":"Pod","namespace":"team-a","name":"mira"`},
		{"an unknown owner", `"owner":"cluster"`, `"owner":"nobody"`},
		{"an object with no status", `,"status":{"phase":"Ready"}`, ``},
		{"not a report", report, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(report, tc.from, tc.to, 1)
			require.NotEqual(t, report, raw, "the case changes the report")
			_, err := cluster.ParseReport([]byte(raw))
			assert.Error(t, err)
		})
	}
	r, err := cluster.ParseReport([]byte(report))
	require.NoError(t, err, "the control: the unchanged report is admitted")
	assert.Len(t, r.Objects, 2)
}

// reports is cluster.ReportStore holding placed as the identifiers
// placed on reporter, and recording what it was asked to keep.
type reports struct {
	cluster.ReportStore
	placed map[string]bool
	asked  []string
	kept   *cluster.Report
	err    error
}

func (s *reports) PlacedDeclarationIDs(_ context.Context, g grn.GRN, ids []string) (map[string]bool, error) {
	s.asked = ids
	out := map[string]bool{}
	for _, id := range ids {
		if g == reporter && s.placed[id] {
			out[id] = true
		}
	}
	return out, nil
}

func (s *reports) KeepReport(_ context.Context, _ grn.GRN, r cluster.Report) error {
	s.kept = &r
	return s.err
}

func names(r *cluster.Report) []string {
	var out []string
	for _, o := range r.Declarations {
		out = append(out, "outcome:"+o.ID)
	}
	for _, o := range r.Objects {
		out = append(out, o.Owner+":"+o.Name)
	}
	return out
}

// TestReport_KeepsOnlyWhatNamesADeclarationPlacedOnTheRecord holds
// REP-8: an outcome or an object naming an identifier not placed on the
// reporting record is kept nowhere, nor a garam-owned object whose
// marker names another record, and a cluster-owned object naming none
// is kept.
func TestReport_KeepsOnlyWhatNamesADeclarationPlacedOnTheRecord(t *testing.T) {
	parse := func(raw string) cluster.Report {
		r, err := cluster.ParseReport([]byte(raw))
		require.NoError(t, err)
		return r
	}
	store := &reports{placed: map[string]bool{"decl-a": true}}
	svc := cluster.NewService(nil, nil, store, cluster.Config{})

	require.NoError(t, svc.Report(context.Background(), reporter, parse(report)))
	assert.Equal(t, []string{"decl-a"}, store.asked)
	assert.Equal(t, []string{"outcome:decl-a", "garam:reviewer", "cluster:mira"}, names(store.kept),
		"the control: everything names a declaration placed here, or none")

	foreign := strings.ReplaceAll(report, `"decl-a"`, `"decl-theirs"`)
	require.NoError(t, svc.Report(context.Background(), reporter, parse(foreign)))
	assert.Equal(t, []string{"cluster:mira"}, names(store.kept), "another record's identifier is dropped")

	marked := strings.Replace(report, `"cluster":"grn:acme/iam/cluster/5f60710a1b2c3d4e"`,
		`"cluster":"grn:acme/iam/cluster/0000000000000000"`, 1)
	require.NoError(t, svc.Report(context.Background(), reporter, parse(marked)))
	assert.Equal(t, []string{"outcome:decl-a", "cluster:mira"}, names(store.kept), "an object marked for another record is dropped")

	unmarked := strings.Replace(report, `"declarationId":"decl-a",`, ``, 1)
	require.NoError(t, svc.Report(context.Background(), reporter, parse(unmarked)))
	assert.Equal(t, []string{"outcome:decl-a", "cluster:mira"}, names(store.kept), "a garam-owned object naming no declaration is dropped")

	claimed := strings.Replace(report, `"owner":"cluster","generation":3,`,
		`"owner":"cluster","generation":3,"declarationId":"decl-theirs",`, 1)
	require.NoError(t, svc.Report(context.Background(), reporter, parse(claimed)))
	assert.Equal(t, []string{"outcome:decl-a", "garam:reviewer"}, names(store.kept),
		"a cluster-owned object naming an identifier not placed here is dropped")
}

func TestReport_ARecordDeletedMeanwhileIsNoClient(t *testing.T) {
	r, err := cluster.ParseReport([]byte(report))
	require.NoError(t, err)
	svc := cluster.NewService(nil, nil, &reports{err: cluster.ErrClusterNotFound}, cluster.Config{})
	assert.ErrorIs(t, svc.Report(context.Background(), reporter, r), operator.ErrClientInvalid)
}

func TestReport_IsBoundedPerCluster(t *testing.T) {
	r, err := cluster.ParseReport([]byte(report))
	require.NoError(t, err)
	svc := cluster.NewService(nil, nil, &reports{}, cluster.Config{})
	for range cluster.ReportsPerCluster {
		require.NoError(t, svc.Report(context.Background(), reporter, r))
	}
	var exceeded *operator.Exceeded
	assert.ErrorAs(t, svc.Report(context.Background(), reporter, r), &exceeded)
	other := grn.New("acme", cluster.ServiceType, "0000000000000001")
	assert.NoError(t, svc.Report(context.Background(), other, r), "the control: another record has its own bound")
}
