package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
)

// TestClusterReport_AnswersTheKeptReportAndTheLatestContact holds the
// browser's read of what a cluster reported (ADR-0188): null for each
// where there is none, then what was kept, a cluster-owned object's
// spec and every status answered as the cluster sent them.
func TestClusterReport_AnswersTheKeptReportAndTheLatestContact(t *testing.T) {
	f := newFixture(t)
	owner, slug, clusterGRN, declarations := declaringOrg(t, f)
	path := declarations[:len(declarations)-len("/declarations")] + "/report"

	rec := f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"cluster":"`+clusterGRN+`","lastContactAt":null,"report":null}`, rec.Body.String())

	g, err := grn.Parse(clusterGRN)
	require.NoError(t, err)
	contact := time.Date(2026, 11, 24, 12, 0, 1, 0, time.UTC)
	f.reports.kept[clusterGRN] = cluster.KeptReport{Cluster: g, LastContactAt: &contact, Report: &cluster.Report{
		ObservedAt: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC),
		Connector:  cluster.ReportConnector{Version: "0.1.0", Mode: "apply", Namespaces: []string{"team-a"}},
		Declarations: []cluster.DeclarationOutcome{
			{ID: "decl-a", Generation: 2, Outcome: "conflict", Message: "<b>held</b>"},
		},
		Objects: []cluster.ReportedObject{{APIVersion: "bot.garam.sh/v1alpha1", Kind: "Bot", Namespace: "team-a",
			Name: "mira", UID: "u2", Owner: cluster.OwnerCluster, Generation: 3,
			Spec: json.RawMessage(`{"persona":{"role":"Lead"}}`), Status: json.RawMessage(`{"phase":"Ready"}`)}},
	}}
	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"cluster":"`+clusterGRN+`","lastContactAt":"2026-11-24T12:00:01Z","report":{
		"observedAt":"2026-11-24T12:00:00Z",
		"connector":{"version":"0.1.0","mode":"apply","namespaces":["team-a"]},
		"declarations":[{"id":"decl-a","generation":2,"outcome":"conflict","message":"<b>held</b>"}],
		"objects":[{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","namespace":"team-a","name":"mira","uid":"u2",
			"owner":"cluster","generation":3,"spec":{"persona":{"role":"Lead"}},"status":{"phase":"Ready"}}]}}`,
		rec.Body.String())

	rec = f.do(t, "GET", "/orgs/"+slug+"/clusters/0000000000000000/report", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "a record the organization does not hold")
}
