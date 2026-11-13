package integration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// patch makes one PATCH as cookie and answers the status and body.
func patch(t *testing.T, base, path, body string, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, base+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// TestPolicyScopedLinks_ARenameIsNotAnsweredByASetAsideMark runs the
// update routes over Postgres, a feed loop and the fake service, with
// the seeded group's mark set aside. A body that wrote no roles is
// answered at once; one that wrote them is answered by what the service
// then says, 503 `change_refused` while it refuses and 200 once it
// accepts (ADR-0162 item 5).
func TestPolicyScopedLinks_ARenameIsNotAnsweredByASetAsideMark(t *testing.T) {
	p, fake, d := setAsideTeam(t)
	execSQL(t, p, `UPDATE org_members SET role = 'owner' WHERE user_grn = $1`, outboxUser)
	_, err := d.pass(t)
	require.NoError(t, err)

	_, engine := policyEngine(t, p)
	token, err := authn.NewToken()
	require.NoError(t, err)
	lee, err := grn.Parse(outboxUser)
	require.NoError(t, err)
	require.NoError(t, authnpg.NewSessionStore(p).Create(context.Background(), token, authn.NewSession(lee, time.Hour)))
	cookie := &http.Cookie{Name: "garam_session", Value: token}

	gate := runLoop(t, p, d, 2*time.Second)
	srv := api.NewServer(userpg.New(p), orgpg.New(p), agentpg.New(p), nil, messagepg.New(p), nil,
		authnpg.NewSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, nil, nil,
		api.Config{SessionTTL: time.Hour}).WithFeed(gate)
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	require.Contains(t, setAsideMarks(t, p), "Iam::ScopedRoleLink "+outboxTeam, "the case needs the mark set aside")

	start := time.Now()
	code, body := patch(t, browser.URL, "/orgs/acme/groups/eng", `{"name":"platform"}`, cookie)
	assert.Equal(t, http.StatusOK, code, body)
	assert.Less(t, time.Since(start), time.Second, "a rename does not wait")

	code, body = patch(t, browser.URL, "/orgs/acme/groups/eng", `{"roles":["owner"]}`, cookie)
	assert.Equal(t, http.StatusServiceUnavailable, code, "a write of the roles is answered by the service")
	assert.Contains(t, body, "change_refused")

	fake.linkRefuseOwner(outboxTeam, nil)
	// The roles differ from the stored ones, as a retry must: a write of
	// the value the group holds marks nothing.
	code, body = patch(t, browser.URL, "/orgs/acme/groups/eng", `{"roles":["admin"]}`, cookie)
	assert.Equal(t, http.StatusOK, code, body)
	assert.Contains(t, fake.links(), scopedLink("ADMIN", outboxTeam), "and the link is in force when it answers")
	assert.NotContains(t, setAsideMarks(t, p), "Iam::ScopedRoleLink "+outboxTeam)
}
