package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
)

type apiArtifact struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Latest int    `json:"latest"`
}

func (s *stack) createArtifact(t *testing.T, who, body string) apiArtifact {
	t.Helper()
	status, got := s.call(t, who, http.MethodPost, "/orgs/acme/artifacts", body)
	require.Equal(t, http.StatusCreated, status, got)
	var a apiArtifact
	require.NoError(t, json.Unmarshal([]byte(got), &a))
	return a
}

// view answers the address an HTML version is viewed at, as who.
func (s *stack) view(t *testing.T, who, id, query string) string {
	t.Helper()
	status, got := s.call(t, who, http.MethodGet, "/orgs/acme/artifacts/"+id+"/view"+query, "")
	require.Equal(t, http.StatusOK, status, got)
	var v struct {
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &v))
	return v.URL
}

// fetch gets a user-content address with no cookie, as the sandboxed
// iframe does, and answers the response and its body.
func fetch(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

// TestArtifacts_EverySaveIsAVersionServedFromTheUserContentOrigin
// keeps three versions of an HTML artifact, each read back from the
// user-content origin under its policy, and never from cowork's own.
func TestArtifacts_EverySaveIsAVersionServedFromTheUserContentOrigin(t *testing.T) {
	s := newStack(t)
	a := s.createArtifact(t, "ada", `{"kind":"html","title":"Chart","html":"<p>v1</p>"}`)
	for _, v := range []string{"v2", "v3"} {
		status, body := s.call(t, "bob", http.MethodPost, "/orgs/acme/artifacts/"+a.ID+"/versions", `{"html":"<p>`+v+`</p>"}`)
		require.Equal(t, http.StatusCreated, status, body)
	}
	status, body := s.call(t, "ada", http.MethodGet, "/orgs/acme/artifacts/"+a.ID+"/versions", "")
	require.Equal(t, http.StatusOK, status, body)
	var vs struct {
		Versions []struct{ Number int } `json:"versions"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &vs))
	require.Len(t, vs.Versions, 3)

	url := s.view(t, "ada", a.ID, "")
	assert.True(t, strings.HasPrefix(url, s.contentURL+"/"), "served from the user-content origin: %s", url)
	resp, got := fetch(t, url)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "<p>v3</p>", got, "the latest")
	csp := resp.Header.Get("Content-Security-Policy")
	assert.Contains(t, csp, "connect-src 'none'")
	assert.Contains(t, csp, "sandbox allow-scripts")

	_, got = fetch(t, s.view(t, "ada", a.ID, "?version=1"))
	assert.Equal(t, "<p>v1</p>", got, "an earlier version stays readable")

	status, _ = s.call(t, "ada", http.MethodGet, "/orgs/acme/artifacts/"+a.ID+"/document", "")
	assert.Equal(t, http.StatusConflict, status, "cowork's own origin serves no html")
}

// TestArtifacts_ADocumentFreezesAPage keeps a page's blocks as they
// stood, and a later save freezes them again.
func TestArtifacts_ADocumentFreezesAPage(t *testing.T) {
	s := newStack(t)
	id := s.createPage(t, "Plan")
	ed := newEditor(t, s, "ada", id)
	ed.connect()
	ed.appendBlock(collab.Block{Type: "paragraph", Text: "first draft"})
	eventually(t, "the edit is kept", func() bool {
		var p mcpPage
		s.agent(t, "ada").ok("read_page", map[string]any{"org": "acme", "page": id}, &p)
		return p.Markdown == "first draft\n"
	})
	a := s.createArtifact(t, "ada", `{"kind":"document","title":"Plan, frozen","page":"`+id+`"}`)
	ed.appendBlock(collab.Block{Type: "paragraph", Text: "later"})

	status, body := s.call(t, "bob", http.MethodGet, "/orgs/acme/artifacts/"+a.ID+"/document", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "first draft")
	assert.NotContains(t, body, "later", "the document is the page as it stood")

	status, body = s.call(t, "ada", http.MethodGet, "/orgs/acme/artifacts/"+a.ID+"/view", "")
	assert.Equal(t, http.StatusBadRequest, status, "a document has no html to view: %s", body)
}

// TestArtifacts_ThePlatformDecidesAndAnAddressIsACapability: carol,
// denied the artifact, gets no address and finds it in no list; an
// address altered, or for another version, is refused by the
// user-content origin.
func TestArtifacts_ThePlatformDecidesAndAnAddressIsACapability(t *testing.T) {
	s := newStack(t)
	a := s.createArtifact(t, "ada", `{"kind":"html","title":"Secret","html":"<p>secret</p>"}`)
	s.platform.denyCheck("carol cowork:artifact-read@" + a.ID)
	s.platform.denyCheck("bob cowork:artifact-write@" + a.ID)

	status, _ := s.call(t, "carol", http.MethodGet, "/orgs/acme/artifacts/"+a.ID+"/view", "")
	assert.Equal(t, http.StatusForbidden, status)
	_, body := s.call(t, "carol", http.MethodGet, "/orgs/acme/artifacts", "")
	assert.NotContains(t, body, a.ID)
	status, _ = s.call(t, "bob", http.MethodPost, "/orgs/acme/artifacts/"+a.ID+"/versions", `{"html":"<p>mine</p>"}`)
	assert.Equal(t, http.StatusForbidden, status)

	url := s.view(t, "ada", a.ID, "")
	resp, _ := fetch(t, strings.Replace(url, "sig=", "sig=A", 1))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "an altered address")
	resp, _ = fetch(t, strings.Replace(url, "/"+a.ID+"/1?", "/"+a.ID+"/2?", 1))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "the address of one version opens no other")
	resp, _ = fetch(t, s.contentURL+"/orgs/acme/artifacts")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "the user-content origin serves nothing of cowork's API")
}

// TestArtifacts_AMessageEmbedsOnlyWhatItsAuthorReads.
func TestArtifacts_AMessageEmbedsOnlyWhatItsAuthorReads(t *testing.T) {
	s := newStack(t)
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	a := s.createArtifact(t, "ada", `{"kind":"html","title":"Chart","html":"<p>chart</p>"}`)
	m := s.post(t, "ada", general.ID, `{"body":"the chart","artifacts":["`+a.ID+`"]}`)
	assert.Equal(t, []string{a.ID}, m.Artifacts)
	_, body := s.call(t, "bob", http.MethodGet, "/orgs/acme/channels/"+general.ID+"/messages", "")
	assert.Contains(t, body, `"artifacts":["`+a.ID+`"]`)

	s.platform.denyCheck("carol cowork:artifact-read@" + a.ID)
	status, _ := s.call(t, "carol", http.MethodPost, "/orgs/acme/channels/"+general.ID+"/messages", `{"body":"x","artifacts":["`+a.ID+`"]}`)
	assert.Equal(t, http.StatusForbidden, status)
}

// TestMCP_AnAgentPublishesAndRevisesAnArtifact.
func TestMCP_AnAgentPublishesAndRevisesAnArtifact(t *testing.T) {
	s := newStack(t)
	bob := s.agent(t, "bob")
	var created struct {
		ID     string `json:"id"`
		Latest int    `json:"latest"`
	}
	bob.ok("create_artifact", map[string]any{"org": "acme", "kind": "html", "title": "Report", "html": "<h1>Q3</h1>"}, &created)
	assert.Equal(t, 1, created.Latest)
	bob.ok("update_artifact", map[string]any{"org": "acme", "artifact": created.ID, "html": "<h1>Q3, revised</h1>"}, &created)
	assert.Equal(t, 2, created.Latest)

	var read struct {
		Version int    `json:"version"`
		HTML    string `json:"html"`
	}
	bob.ok("read_artifact", map[string]any{"org": "acme", "artifact": created.ID, "version": 1}, &read)
	assert.Equal(t, "<h1>Q3</h1>", read.HTML)
	_, got := fetch(t, s.view(t, "ada", created.ID, ""))
	assert.Equal(t, "<h1>Q3, revised</h1>", got, "ada views what bob's agent saved")

	s.platform.denyCheck("carol cowork:artifact-write@" + created.ID)
	res := s.agent(t, "carol").call("update_artifact", map[string]any{"org": "acme", "artifact": created.ID, "html": "x"})
	assert.True(t, res.IsError)
}
