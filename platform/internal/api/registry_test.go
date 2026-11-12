package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
)

func describePath(name string) string {
	return "/registry/grn?grn=" + url.QueryEscape(name)
}

// TestGetRegistry_AnswersWhatTheCatalogueHolds reads the answer against
// lib/grn's generated catalogue both ways: a service, type or action
// the answer holds and the catalogue does not fails it, and so does
// one the catalogue holds and the answer omits.
func TestGetRegistry_AnswersWhatTheCatalogueHolds(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "alice", "Alice")

	rec := f.do(t, "GET", "/registry", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.Registry](t, rec)

	var services []string
	var actions []string
	answered := map[string]apigen.RegistryAction{}
	for _, service := range got.Services {
		services = append(services, service.Name)

		want := grn.Types(service.Name)
		require.NotEmpty(t, want, "the catalogue declares %s", service.Name)
		gotTypes := map[string]grn.TypeInfo{}
		for _, typ := range service.Types {
			gotTypes[typ.Name] = grn.TypeInfo{Entity: typ.EntityType, Parents: typ.Parents}
		}
		assert.Equal(t, want, gotTypes, "types of %s", service.Name)

		for _, action := range service.Actions {
			actions = append(actions, action.Name)
			answered[action.Name] = action
			assert.True(t, strings.HasPrefix(action.Name, service.Name+":"), "%s is listed under %s", action.Name, service.Name)
		}
	}
	assert.Equal(t, grn.Services(), services)

	var want []string
	for _, action := range grn.Actions() {
		want = append(want, action.Name)
		assert.Equal(t, action.Principals, answered[action.Name].Principals, action.Name)
		assert.Equal(t, action.Resources, answered[action.Name].Resources, action.Name)
	}
	slices.Sort(actions)
	slices.Sort(want)
	assert.Equal(t, want, actions)
}

func TestDescribeGrn(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "alice", "Alice")

	describe := func(t *testing.T, name string) apigen.GrnDescription {
		t.Helper()
		rec := f.do(t, "GET", describePath(name), "", cookie)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return decodeBody[apigen.GrnDescription](t, rec)
	}

	t.Run("a platform name", func(t *testing.T) {
		d := describe(t, "grn:acme/iam/group/eng")
		assert.True(t, d.Known)
		assert.Equal(t, "grn:acme/iam/group/eng", d.Grn)
		assert.Equal(t, "iam", *d.Service)
		assert.Equal(t, "group", *d.Type)
		assert.Equal(t, "Iam::Team", *d.EntityType)
		assert.Nil(t, d.Parent, "a name of one pair has no parent")
		assert.Nil(t, d.Locator)
		assert.Contains(t, *d.Actions, "iam:group.member-add")
		assert.NotContains(t, *d.Actions, "iam:agent.read", "an action of another type does not apply")
	})

	t.Run("a cowork name nested under a project", func(t *testing.T) {
		d := describe(t, "grn:acme/cowork/project/frontend/channel/general")
		assert.True(t, d.Known)
		assert.Equal(t, "cowork", *d.Service)
		assert.Equal(t, "channel", *d.Type)
		assert.Equal(t, "Cowork::Channel", *d.EntityType)
		assert.Equal(t, "grn:acme/cowork/project/frontend", *d.Parent)
		assert.Contains(t, *d.Actions, "cowork:channel.post")
		assert.NotContains(t, *d.Actions, "cowork:page.read")
	})

	t.Run("a name with a locator", func(t *testing.T) {
		d := describe(t, "grn:acme/cowork/page/home#section-2")
		assert.True(t, d.Known)
		assert.Equal(t, "grn:acme/cowork/page/home#section-2", d.Grn)
		assert.Equal(t, "page", *d.Type)
		assert.Equal(t, "section-2", *d.Locator)
		assert.Nil(t, d.Parent, "a locator is not a pair")
	})

	t.Run("a service no manifest declares is unknown", func(t *testing.T) {
		d := describe(t, "grn:acme/codehub/repo/api-server")
		assert.False(t, d.Known)
		assert.Equal(t, "grn:acme/codehub/repo/api-server", d.Grn)
		assert.Nil(t, d.Service)
		assert.Nil(t, d.Type)
		assert.Nil(t, d.EntityType)
		assert.Nil(t, d.Actions)
	})

	t.Run("a type its service does not declare is unknown", func(t *testing.T) {
		assert.False(t, describe(t, "grn:acme/iam/widget/x").Known)
		assert.False(t, describe(t, "grn:acme/cowork/user/alice").Known, "a type of another service")
	})

	t.Run("what applies is what the catalogue says applies", func(t *testing.T) {
		for _, name := range []string{
			"grn:root/iam/org/acme", "grn:acme/iam/agent/bot-1", "grn:acme/cowork/task/t1",
		} {
			g, err := grn.Parse(name)
			require.NoError(t, err)
			entity := grn.Types(g.Service)[g.ServiceType].Entity
			var want []string
			for _, action := range grn.Actions() {
				if slices.Contains(action.Resources, entity) {
					want = append(want, action.Name)
				}
			}
			slices.Sort(want)
			require.NotEmpty(t, want, name)
			assert.Equal(t, want, *describe(t, name).Actions, name)
		}
	})
}

// TestDescribeGrn_RefusesWhatIsNoGRN holds the 400 for a string that is
// no GRN beside the 200 for a name no manifest declares: the refusal is
// not the answer unknown gives, and the answer unknown gives is not a
// refusal.
func TestDescribeGrn_RefusesWhatIsNoGRN(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "alice", "Alice")

	for _, name := range []string{
		"hello", "arn:acme/iam/user/alice", "grn:acme/iam/user", "grn:acme:default:user:alice",
		"grn:AC/iam/user/alice", "grn:acme/iam/user/al?ice",
	} {
		rec := f.do(t, "GET", describePath(name), "", cookie)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Equal(t, "invalid_argument", decodeBody[apigen.Error](t, rec).Kind, name)
	}

	rec := f.do(t, "GET", "/registry/grn", "", cookie)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "no name asked")

	control := f.do(t, "GET", describePath("grn:acme/nosuch/thing/x"), "", cookie)
	assert.Equal(t, http.StatusOK, control.Code, "an undeclared service is answered, not refused")
}

// TestRegistry_NeedsASessionAndNothingMore holds both operations to a
// signed-in caller, whatever the caller's role: the fixture's evaluator
// is a strict mock with no expectation, so a permission check would
// fail the test.
func TestRegistry_NeedsASessionAndNothingMore(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "alice", "Alice")

	for _, path := range []string{"/registry", describePath("grn:acme/iam/user/alice")} {
		assert.Equal(t, http.StatusUnauthorized, f.do(t, "GET", path, "").Code, "%s without a session", path)
		assert.Equal(t, http.StatusOK, f.do(t, "GET", path, "", cookie).Code, "%s with one", path)

		bearer := httptest.NewRequest("GET", path, nil)
		bearer.Header.Set("Authorization", "Bearer not-a-session")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, bearer)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s with a bearer no session holds", path)
	}
}

// TestRegistry_PrintsEachOperationAsServed prints the status line,
// headers and body of each operation, served over a real listener by
// the handler `serve api` mounts. It needs no store: run it with
//
//	go test -v -run TestRegistry_PrintsEachOperationAsServed ./internal/api/
//
// from platform/, since the suite runs without -v and a passing test's
// log is not shown.
func TestRegistry_PrintsEachOperationAsServed(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "alice", "Alice")
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)

	for _, path := range []string{
		"/registry",
		describePath("grn:acme/iam/group/eng"),
		describePath("grn:acme/cowork/project/frontend/channel/general"),
		describePath("grn:acme/cowork/page/home#section-2"),
		describePath("grn:acme/codehub/repo/api-server"),
		describePath("not-a-grn"),
	} {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		req.AddCookie(cookie)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		dump, err := httputil.DumpResponse(resp, true)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		t.Logf("GET %s\n%s", path, dump)
	}
}
