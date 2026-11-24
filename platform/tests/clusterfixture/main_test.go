package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSession = "session-value"
	testRecord  = "grn:acme/iam/cluster/5f60710a1b2c3d4e"
)

// browserAPI stands in for serve api's cluster routes: it answers the
// session it was given and records each request it served.
type browserAPI struct {
	mu       sync.Mutex
	requests []string
	bodies   []string
	keys     string
}

func (b *browserAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value != testSession {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"kind":"unauthenticated","message":"sign in"}`))
		return
	}
	body, _ := io.ReadAll(r.Body)
	b.mu.Lock()
	b.requests = append(b.requests, r.Method+" "+r.URL.Path)
	if len(body) > 0 {
		b.bodies = append(b.bodies, string(body))
	}
	b.mu.Unlock()
	record := "/orgs/acme/clusters/5f60710a1b2c3d4e"
	switch r.Method + " " + r.URL.Path {
	case "POST /orgs/acme/clusters":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"cluster":{"grn":"` + testRecord + `"},"registration":{"registrationToken":"garam_crt_new"}}`))
	case "POST " + record + "/registration-token":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"registrationToken":"garam_crt_reissued"}`))
	case "GET " + record + "/keys":
		_, _ = w.Write([]byte(b.keys))
	case "DELETE " + record + "/keys/live", "DELETE " + record + "/keys/retiring", "DELETE " + record,
		"DELETE " + record + "/declarations/chosen":
		w.WriteHeader(http.StatusNoContent)
	case "GET /orgs/acme/clusters":
		_, _ = w.Write([]byte(`[{"grn":"grn:acme/iam/cluster/0000000000000000"},{"grn":"` + testRecord + `"}]`))
	case "GET " + record:
		_, _ = w.Write([]byte(`{"grn":"` + testRecord + `"}`))
	case "POST " + record + "/declarations":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"decl-drawn","generation":1}`))
	case "PUT " + record + "/declarations/chosen":
		_, _ = w.Write([]byte(`{"id":"chosen","generation":4}`))
	case "GET " + record + "/declarations/chosen":
		_, _ = w.Write([]byte(`{"id":"chosen","generation":4}`))
	case "GET " + record + "/report":
		_, _ = w.Write([]byte(`{"cluster":"` + testRecord + `","lastContactAt":"2026-11-24T12:00:01Z","report":null}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"not_found","message":"cluster not found"}`))
	}
}

// runFixture runs one subcommand against api with the settings env
// adds, and answers the exit code and what it printed.
func runFixture(t *testing.T, api string, env map[string]string, args ...string) (int, map[string]any, string) {
	t.Helper()
	settings := map[string]string{envAPIURL: api, envSessionToken: testSession, envOrg: "acme"}
	for k, v := range env {
		settings[k] = v
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, func(k string) string { return settings[k] }, &stdout, &stderr)
	var answer map[string]any
	if stdout.Len() > 0 {
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &answer), stdout.String())
	}
	return code, answer, stdout.String() + stderr.String()
}

// TestMintToken_AnswersANewRecordsTokenOrTheNamedRecordsReissue holds
// mint-token's two forms against the routes each takes.
func TestMintToken_AnswersANewRecordsTokenOrTheNamedRecordsReissue(t *testing.T) {
	api := &browserAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	code, answer, _ := runFixture(t, srv.URL, nil, "mint-token")
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"registrationToken": "garam_crt_new", "cluster": testRecord}, answer)

	code, answer, _ = runFixture(t, srv.URL, nil, "mint-token", "--cluster", testRecord)
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"registrationToken": "garam_crt_reissued", "cluster": testRecord}, answer)
}

// TestMintToken_RefusesAnExpiredMintAgainstADatabaseOffThisMachine
// holds that --expired refuses before anything is minted, and that the
// same mint without --expired reaches garam, the control.
func TestMintToken_RefusesAnExpiredMintAgainstADatabaseOffThisMachine(t *testing.T) {
	api := &browserAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	for name, url := range map[string]string{
		"no database":      "",
		"a remote server":  "postgres://u:p@db.example.com:5432/garam",
		"a remote address": "postgres://u:p@10.0.0.5:5432/garam",
	} {
		code, answer, out := runFixture(t, srv.URL, map[string]string{envDatabaseURL: url}, "mint-token", "--expired")
		assert.Equal(t, 1, code, name)
		assert.Equal(t, "invalid_request", answer["error"], name)
		assert.NotContains(t, out, "u:p", name+": the connection string is not repeated")
	}
	assert.Empty(t, api.requests, "a refused --expired minted nothing")

	code, _, _ := runFixture(t, srv.URL, nil, "mint-token")
	assert.Equal(t, 0, code)
	assert.Len(t, api.requests, 1)
}

// TestRevokeKeys_WithdrawsEveryKeyNotYetWithdrawn holds that a revoked
// key is left alone and every other is withdrawn.
func TestRevokeKeys_WithdrawsEveryKeyNotYetWithdrawn(t *testing.T) {
	api := &browserAPI{keys: `{"keys":[{"kid":"live","state":"active"},{"kid":"retiring","state":"retiring"},` +
		`{"kid":"gone","state":"revoked"}]}`}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	code, answer, _ := runFixture(t, srv.URL, nil, "revoke-keys", testRecord)
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"cluster": testRecord}, answer)
	record := "/orgs/acme/clusters/5f60710a1b2c3d4e"
	assert.Equal(t, []string{"GET " + record + "/keys", "DELETE " + record + "/keys/live",
		"DELETE " + record + "/keys/retiring"}, api.requests)
}

// TestFixture_PrintsARefusalInTheInterfacesErrorBody holds the
// fixture's failure form (conformance.md §The server fixture): a
// refusal exits 1 printing T-5's body, coded by garam's status, and a
// command line it cannot read exits 2.
func TestFixture_PrintsARefusalInTheInterfacesErrorBody(t *testing.T) {
	srv := httptest.NewServer(&browserAPI{})
	t.Cleanup(srv.Close)

	code, answer, _ := runFixture(t, srv.URL, nil, "delete-cluster", "grn:acme/iam/cluster/0000000000000000")
	assert.Equal(t, 1, code)
	assert.Equal(t, map[string]any{"error": "not_found", "message": "cluster not found"}, answer)

	code, answer, out := runFixture(t, srv.URL, map[string]string{envSessionToken: "stale"}, "delete-cluster", testRecord)
	assert.Equal(t, 1, code)
	assert.Equal(t, "unauthorized", answer["error"])
	assert.NotContains(t, out, "stale", "the session is not repeated")

	code, answer, _ = runFixture(t, srv.URL, nil, "revoke-keys", "grn:acme/iam/operator/one")
	assert.Equal(t, 1, code)
	assert.Equal(t, "invalid_request", answer["error"], "an operator is not a record")

	code, answer, _ = runFixture(t, srv.URL, map[string]string{envOrg: ""}, "mint-token")
	assert.Equal(t, 1, code)
	assert.Equal(t, "invalid_request", answer["error"])

	for _, args := range [][]string{{}, {"declare"}, {"tombstone"}, {"tombstone", "a", "b"}, {"expire-cursors"},
		{"delete-cluster"}, {"mint-token", "extra"}} {
		code, _, out := runFixture(t, srv.URL, nil, args...)
		assert.Equal(t, 2, code, strings.Join(args, " "))
		assert.Contains(t, out, "usage:")
	}

	code, _, _ = runFixture(t, srv.URL, nil, "delete-cluster", testRecord)
	assert.Equal(t, 0, code, "the control: the record's own delete succeeds")
}

// declareFile writes a declare FILE into the test's directory.
func writeDeclareFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "declare.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestDeclare_PlacesTheFilesObjectUnderItsIdentifierOrADrawnOne holds
// declare's two forms against the routes each takes, the object sent
// on as the file carries it.
func TestDeclare_PlacesTheFilesObjectUnderItsIdentifierOrADrawnOne(t *testing.T) {
	api := &browserAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	object := `{"kind":"Bot","spec":{"replicas":12345678901234567890}}`

	code, answer, _ := runFixture(t, srv.URL, nil, "declare", writeDeclareFile(t, `{"cluster":"`+testRecord+`","object":`+object+`}`))
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"id": "decl-drawn", "generation": float64(1)}, answer)

	code, answer, _ = runFixture(t, srv.URL, nil, "declare",
		writeDeclareFile(t, `{"cluster":"`+testRecord+`","id":"chosen","object":`+object+`}`))
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"id": "chosen", "generation": float64(4)}, answer)
	record := "/orgs/acme/clusters/5f60710a1b2c3d4e"
	assert.Equal(t, []string{"POST " + record + "/declarations", "PUT " + record + "/declarations/chosen"}, api.requests)
	for _, body := range api.bodies {
		assert.JSONEq(t, `{"object":`+object+`}`, body)
		assert.Contains(t, body, "12345678901234567890", "the object is sent on unrounded")
	}

	code, answer, _ = runFixture(t, srv.URL, nil, "declare", writeDeclareFile(t, `{"cluster":"grn:acme/iam/operator/one","object":{}}`))
	assert.Equal(t, 1, code)
	assert.Equal(t, "invalid_request", answer["error"], "an operator is not a record")
	code, answer, _ = runFixture(t, srv.URL, nil, "declare", filepath.Join(t.TempDir(), "missing.json"))
	assert.Equal(t, 1, code)
	assert.Equal(t, "invalid_request", answer["error"], "a file that is not there")
}

// TestTombstone_FindsTheRecordHoldingTheIdentifier holds that
// tombstone searches the organization's records, skips one that holds
// no such declaration, and answers the generation the delete carries.
func TestTombstone_FindsTheRecordHoldingTheIdentifier(t *testing.T) {
	api := &browserAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	code, answer, _ := runFixture(t, srv.URL, nil, "tombstone", "chosen")
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"id": "chosen", "generation": float64(5)}, answer)
	record := "/orgs/acme/clusters/5f60710a1b2c3d4e"
	assert.Equal(t, []string{"GET /orgs/acme/clusters", "GET /orgs/acme/clusters/0000000000000000/declarations/chosen",
		"GET " + record + "/declarations/chosen", "DELETE " + record + "/declarations/chosen"}, api.requests)

	code, answer, _ = runFixture(t, srv.URL, nil, "tombstone", "absent")
	assert.Equal(t, 1, code)
	assert.Equal(t, "not_found", answer["error"])
}

// TestLatestReport_PrintsWhatGetClusterReportAnswers holds the members
// the suite reads, a report the record has none of as null, and a
// record the organization does not hold refused.
func TestLatestReport_PrintsWhatGetClusterReportAnswers(t *testing.T) {
	api := &browserAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	code, answer, _ := runFixture(t, srv.URL, nil, "latest-report", testRecord)
	require.Equal(t, 0, code)
	assert.Equal(t, map[string]any{"cluster": testRecord, "lastContactAt": "2026-11-24T12:00:01Z", "report": nil}, answer)
	assert.Equal(t, []string{"GET /orgs/acme/clusters/5f60710a1b2c3d4e/report"}, api.requests)

	code, answer, _ = runFixture(t, srv.URL, nil, "latest-report", "grn:acme/iam/cluster/0000000000000000")
	assert.Equal(t, 1, code)
	assert.Equal(t, "not_found", answer["error"])
}

// TestExpireCursors_RefusesADatabaseOffThisMachineBeforeAnything holds
// that expire-cursors checks its database before it reaches garam.
func TestExpireCursors_RefusesADatabaseOffThisMachineBeforeAnything(t *testing.T) {
	api := &browserAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	for name, url := range map[string]string{
		"no database":     "",
		"a remote server": "postgres://u:p@db.example.com:5432/garam",
	} {
		code, answer, out := runFixture(t, srv.URL, map[string]string{envDatabaseURL: url}, "expire-cursors", testRecord)
		assert.Equal(t, 1, code, name)
		assert.Equal(t, "invalid_request", answer["error"], name)
		assert.NotContains(t, out, "u:p", name+": the connection string is not repeated")
	}
	assert.Empty(t, api.requests, "a refused expire-cursors reached nothing")
}
