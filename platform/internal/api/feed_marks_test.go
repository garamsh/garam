package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	"github.com/garamsh/garam/platform/internal/user"
)

// mark is a mark of kind typ for the entity id.
func mark(typ, id string) policyfeed.Entity { return policyfeed.Entity{Type: typ, ID: id} }

// bodyGRN reads the GRN of the entity a create answered with.
func bodyGRN(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeBody[map[string]any](t, rec)["grn"].(string)
}

// feedRoute is one request that changes what the policy service decides
// from: how to send it, what it answers once the service holds the
// change, and the marks it asks the gate about (ADR-0165).
type feedRoute struct {
	name string
	// send issues the request; n makes what it creates unique.
	send func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder
	// seed stores what send changes, before the gate is made to fail.
	seed   func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int)
	status int
	// marks is the whole marks, kind and id, the request must name,
	// read from what send answered and what seed stored.
	marks func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int, rec *httptest.ResponseRecorder) []policyfeed.Entity
}

func feedRoutes() []feedRoute {
	memberPath := func(slug, kind, id string, member grn.GRN) string {
		return "/orgs/" + slug + "/" + kind + "/" + id + "/members/" + url.PathEscape(member.String())
	}
	member := func(slug, kind string, n int) grn.GRN {
		return grn.New(slug, kind, fmt.Sprintf("m%d", n))
	}
	groupFor := func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) string {
		return createGroup(t, f, owner, slug, fmt.Sprintf("held%d", n))
	}
	projectFor := func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) string {
		rec := f.do(t, "POST", "/orgs/"+slug+"/projects", fmt.Sprintf(`{"name":"held%d"}`, n), owner)
		require.Equal(t, http.StatusCreated, rec.Code)
		g, err := grn.Parse(decodeBody[apigen.Project](t, rec).Grn)
		require.NoError(t, err)
		return g.Identifier
	}
	groupMark := func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) string {
		return grn.New(slug, org.ServiceTypeGroup, groupIDFor(t, f, owner, slug, n)).String()
	}
	projectMark := func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) string {
		return grn.New(slug, org.ServiceTypeProject, projectIDFor(t, f, owner, slug, n)).String()
	}
	// deleted holds the GRN of what a delete route's seed stored: what
	// is gone cannot be listed to name its marks.
	deleted := map[string]string{}
	one := func(typ string, id func(slug string, n int) string) func(*testing.T, *fixture, *http.Cookie, string, int, *httptest.ResponseRecorder) []policyfeed.Entity {
		return func(_ *testing.T, _ *fixture, _ *http.Cookie, slug string, n int, _ *httptest.ResponseRecorder) []policyfeed.Entity {
			return []policyfeed.Entity{mark(typ, id(slug, n))}
		}
	}
	return []feedRoute{
		{name: "createOrg", status: http.StatusCreated,
			marks: func(t *testing.T, _ *fixture, _ *http.Cookie, _ string, _ int, rec *httptest.ResponseRecorder) []policyfeed.Entity {
				created := bodyGRN(t, rec)
				owner := userGRNOf("owner-sub").String()
				return []policyfeed.Entity{
					mark(policyfeed.OrgType, created), mark(policyfeed.OrgMembersType, created),
					mark(policyfeed.RoleLinkType, created+" "+owner), mark(policyfeed.UserType, owner),
				}
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, _ string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs", fmt.Sprintf(`{"name":"made%d"}`, n), owner)
			}},
		{name: "createGroup", status: http.StatusCreated,
			marks: func(t *testing.T, _ *fixture, _ *http.Cookie, _ string, _ int, rec *httptest.ResponseRecorder) []policyfeed.Entity {
				created := bodyGRN(t, rec)
				return []policyfeed.Entity{mark(policyfeed.TeamType, created), mark(policyfeed.ScopedRoleLinkType, created)}
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/groups", fmt.Sprintf(`{"name":"g%d"}`, n), owner)
			}},
		{name: "createProject", status: http.StatusCreated,
			marks: func(t *testing.T, _ *fixture, _ *http.Cookie, _ string, _ int, rec *httptest.ResponseRecorder) []policyfeed.Entity {
				created := bodyGRN(t, rec)
				return []policyfeed.Entity{mark(policyfeed.ProjectType, created), mark(policyfeed.ScopedRoleLinkType, created)}
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/projects", fmt.Sprintf(`{"name":"p%d"}`, n), owner)
			}},
		{name: "addGroupMember user", status: http.StatusNoContent,
			marks: one(policyfeed.UserType, func(slug string, n int) string { return member(slug, user.ServiceType, n).String() }),
			seed:  func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) { groupFor(t, f, owner, slug, n) },
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/groups/"+groupIDFor(t, f, owner, slug, n)+"/members",
					fmt.Sprintf(`{"memberGrn":%q}`, member(slug, user.ServiceType, n)), owner)
			}},
		{name: "addGroupMember agent", status: http.StatusNoContent,
			marks: one(policyfeed.AgentType, func(slug string, n int) string { return member(slug, agent.ServiceType, n).String() }),
			seed:  func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) { groupFor(t, f, owner, slug, n) },
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/groups/"+groupIDFor(t, f, owner, slug, n)+"/members",
					fmt.Sprintf(`{"memberGrn":%q}`, member(slug, agent.ServiceType, n)), owner)
			}},
		{name: "removeGroupMember", status: http.StatusNoContent,
			marks: one(policyfeed.UserType, func(slug string, n int) string { return member(slug, user.ServiceType, n).String() }),
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) {
				id := groupFor(t, f, owner, slug, n)
				rec := f.do(t, "POST", "/orgs/"+slug+"/groups/"+id+"/members",
					fmt.Sprintf(`{"memberGrn":%q}`, member(slug, user.ServiceType, n)), owner)
				require.Equal(t, http.StatusNoContent, rec.Code)
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "DELETE", memberPath(slug, "groups", groupIDFor(t, f, owner, slug, n),
					member(slug, user.ServiceType, n)), "", owner)
			}},
		{name: "addProjectMember user", status: http.StatusNoContent,
			marks: one(policyfeed.UserType, func(slug string, n int) string { return member(slug, user.ServiceType, n).String() }),
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) {
				projectFor(t, f, owner, slug, n)
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/projects/"+projectIDFor(t, f, owner, slug, n)+"/members",
					fmt.Sprintf(`{"memberGrn":%q}`, member(slug, user.ServiceType, n)), owner)
			}},
		{name: "removeProjectMember", status: http.StatusNoContent,
			marks: one(policyfeed.AgentType, func(slug string, n int) string { return member(slug, agent.ServiceType, n).String() }),
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) {
				id := projectFor(t, f, owner, slug, n)
				rec := f.do(t, "POST", "/orgs/"+slug+"/projects/"+id+"/members",
					fmt.Sprintf(`{"memberGrn":%q}`, member(slug, agent.ServiceType, n)), owner)
				require.Equal(t, http.StatusNoContent, rec.Code)
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "DELETE", memberPath(slug, "projects", projectIDFor(t, f, owner, slug, n),
					member(slug, agent.ServiceType, n)), "", owner)
			}},
		{name: "registerOperator", status: http.StatusCreated,
			marks: one(policyfeed.OperatorType, func(slug string, n int) string {
				return grn.New(slug, operator.ServiceType, fmt.Sprintf("op%d", n)).String()
			}),
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/operators", fmt.Sprintf(`{"identifier":"op%d"}`, n), owner)
			}},
		{name: "registerOperator pair", status: http.StatusCreated,
			marks: func(_ *testing.T, _ *fixture, _ *http.Cookie, slug string, n int, _ *httptest.ResponseRecorder) []policyfeed.Entity {
				return []policyfeed.Entity{
					mark(policyfeed.OperatorType, grn.New(slug, operator.ServiceType, fmt.Sprintf("pair%d", n)).String()),
					mark(policyfeed.OperatorType, grn.New(slug, operator.ServiceType, fmt.Sprintf("pair%d-control", n)).String()),
				}
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "POST", "/orgs/"+slug+"/operators", fmt.Sprintf(
					`{"identifier":"pair%d","controlService":{"identifier":"pair%d-control","endpoint":"https://control.example/"}}`,
					n, n), owner)
			}},
		{name: "deleteOperator", status: http.StatusNoContent,
			marks: one(policyfeed.OperatorType, func(slug string, n int) string {
				return grn.New(slug, operator.ServiceType, fmt.Sprintf("gone%d", n)).String()
			}),
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) {
				rec := f.do(t, "POST", "/orgs/"+slug+"/operators", fmt.Sprintf(`{"identifier":"gone%d"}`, n), owner)
				require.Equal(t, http.StatusCreated, rec.Code)
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "DELETE", "/orgs/"+slug+"/operators/"+fmt.Sprintf("gone%d", n), "", owner)
			}},
		{name: "deleteGroup", status: http.StatusNoContent,
			marks: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int, _ *httptest.ResponseRecorder) []policyfeed.Entity {
				// The group is gone, so its mark is read from the id the
				// seed stored before the send.
				id := deleted[fmt.Sprintf("group%d", n)]
				return []policyfeed.Entity{mark(policyfeed.TeamType, id), mark(policyfeed.ScopedRoleLinkType, id)}
			},
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) {
				groupFor(t, f, owner, slug, n)
				deleted[fmt.Sprintf("group%d", n)] = groupMark(t, f, owner, slug, n)
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "DELETE", "/orgs/"+slug+"/groups/"+groupIDFor(t, f, owner, slug, n), "", owner)
			}},
		{name: "deleteProject", status: http.StatusNoContent,
			marks: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int, _ *httptest.ResponseRecorder) []policyfeed.Entity {
				id := deleted[fmt.Sprintf("project%d", n)]
				return []policyfeed.Entity{mark(policyfeed.ProjectType, id), mark(policyfeed.ScopedRoleLinkType, id)}
			},
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) {
				projectFor(t, f, owner, slug, n)
				deleted[fmt.Sprintf("project%d", n)] = projectMark(t, f, owner, slug, n)
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) *httptest.ResponseRecorder {
				return f.do(t, "DELETE", "/orgs/"+slug+"/projects/"+projectIDFor(t, f, owner, slug, n), "", owner)
			}},
		{name: "deleteOrg", status: http.StatusNoContent,
			marks: func(_ *testing.T, _ *fixture, _ *http.Cookie, _ string, n int, _ *httptest.ResponseRecorder) []policyfeed.Entity {
				id := deleted[fmt.Sprintf("org%d", n)]
				return []policyfeed.Entity{mark(policyfeed.OrgType, id), mark(policyfeed.OrgMembersType, id)}
			},
			seed: func(t *testing.T, f *fixture, owner *http.Cookie, _ string, n int) {
				deleted[fmt.Sprintf("org%d", n)] = grn.Org(createOrg(t, f, owner, fmt.Sprintf("doomed%d", n))).String()
			},
			send: func(t *testing.T, f *fixture, owner *http.Cookie, _ string, n int) *httptest.ResponseRecorder {
				parsed, err := grn.Parse(deleted[fmt.Sprintf("org%d", n)])
				require.NoError(t, err)
				return f.do(t, "DELETE", "/orgs/"+parsed.Identifier, "", owner)
			}},
	}
}

// groupIDFor and projectIDFor find what a route's seed created for n.
func groupIDFor(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) string {
	t.Helper()
	return idByName(t, f, owner, "/orgs/"+slug+"/groups", fmt.Sprintf("held%d", n))
}

func projectIDFor(t *testing.T, f *fixture, owner *http.Cookie, slug string, n int) string {
	t.Helper()
	return idByName(t, f, owner, "/orgs/"+slug+"/projects", fmt.Sprintf("held%d", n))
}

// idByName reads the identifier of the listed entity called name.
func idByName(t *testing.T, f *fixture, owner *http.Cookie, path, name string) string {
	t.Helper()
	rec := f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	for _, e := range decodeBody[[]map[string]any](t, rec) {
		if e["name"] == name {
			g, err := grn.Parse(e["grn"].(string))
			require.NoError(t, err)
			return g.Identifier
		}
	}
	require.FailNow(t, "not listed", name)
	return ""
}

// allPermitted is an organization owned by "owner-sub" who may do every
// thing the routes under test do.
func allPermitted(t *testing.T, f *fixture) (*http.Cookie, string) {
	t.Helper()
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	g := userGRNOf("owner-sub")
	f.permit(
		permission{g, org.ActionGroupCreate}, permission{g, org.ActionGroupRead}, permission{g, org.ActionGroupUpdate},
		permission{g, org.ActionGroupMemberAdd}, permission{g, org.ActionGroupMemberRemove},
		permission{g, org.ActionProjectCreate}, permission{g, org.ActionProjectRead}, permission{g, org.ActionProjectUpdate},
		permission{g, org.ActionProjectMemberAdd}, permission{g, org.ActionProjectMemberRemove},
		permission{g, org.ActionOperatorRegister}, permission{g, org.ActionOperatorDelete},
		permission{g, org.ActionOperatorUpdate}, permission{g, org.ActionOrgUpdate},
		permission{g, org.ActionGroupDelete}, permission{g, org.ActionProjectDelete},
		permission{g, org.ActionOrgDelete})
	// Asking once keeps the stubbed evaluator's expectation met for a
	// route that asks it nothing.
	require.Equal(t, http.StatusOK, f.do(t, "GET", "/orgs/"+slug+"/groups", "", owner).Code)
	return owner, slug
}

// TestFedRoutes_AnswerAfterTheMarksTheirWriteRaised holds, per route,
// that a request that changes what the policy service decides from asks
// the gate about the marks its own write raised (ADR-0165 item 2), and
// that at the bound the answer is 503 `change_not_applied` while the
// change stays stored.
//
// The clear gate is the control: the same route answers its own status
// and names the marks, so the 503 is the wait and not the route.
func TestFedRoutes_AnswerAfterTheMarksTheirWriteRaised(t *testing.T) {
	for _, route := range feedRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f := newFixture(t)
			owner, slug := allPermitted(t, f)
			gate := &recordingGate{}
			f.server.WithFeed(gate)
			if route.seed != nil {
				route.seed(t, f, owner, slug, 1)
				route.seed(t, f, owner, slug, 2)
			}
			gate.marked = nil

			rec := route.send(t, f, owner, slug, 1)
			require.Equal(t, route.status, rec.Code, "the change is in force")
			require.NotEmpty(t, gate.marked, "the answer waited")
			assert.ElementsMatch(t, route.marks(t, f, owner, slug, 1, rec), gate.marked[len(gate.marked)-1],
				"the whole marks, kind and id, its own write raised")

			gate.err = policyfeed.ErrNotApplied
			rec = route.send(t, f, owner, slug, 2)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			body := decodeBody[apigen.Error](t, rec)
			require.NotNil(t, body.Reason)
			assert.Equal(t, policyfeed.ReasonChangeNotApplied, *body.Reason)
			require.NotNil(t, body.Resource)
			assert.Equal(t, "internal error", body.Message, "no text of the chain")
		})
	}
}

// TestFedRoutes_WaitForNothingWithoutAFeed holds that every route above
// answers as it did before the feed existed when the server has no
// gate.
func TestFedRoutes_WaitForNothingWithoutAFeed(t *testing.T) {
	for _, route := range feedRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f := newFixture(t)
			owner, slug := allPermitted(t, f)
			if route.seed != nil {
				route.seed(t, f, owner, slug, 1)
			}

			rec := route.send(t, f, owner, slug, 1)

			assert.Equal(t, route.status, rec.Code)
		})
	}
}

// TestAcceptInvitation_AndRegistration_NameTheUserMark holds that a join
// and a registration wait for the user's entity as well as the link:
// the user is a parent of the role link.
func TestAcceptInvitation_AndRegistration_NameTheUserMark(t *testing.T) {
	f := newFixture(t)
	inviteTo(t, f, "acme", "tok-join")
	inviteTo(t, f, "globex", "tok-reg")
	joiner := f.sessionCookieFor(t, "joiner-sub", "Joiner")
	gate := &recordingGate{}
	f.server.WithFeed(gate)

	rec := f.do(t, "POST", "/invitations/tok-join/accept", "", joiner)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, gate.marked, 1)
	assert.Equal(t, []policyfeed.Entity{
		{Type: policyfeed.RoleLinkType, ID: grn.Org("acme").String() + " " + userGRNOf("joiner-sub").String()},
		{Type: policyfeed.UserType, ID: userGRNOf("joiner-sub").String()},
	}, gate.marked[0])

	rec = f.do(t, "POST", "/auth/password/register", registerBody("tok-reg", "alice", registeredPassword))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, gate.marked, 2)
	assert.Equal(t, []policyfeed.Entity{
		{Type: policyfeed.RoleLinkType, ID: grn.Org("globex").String() + " grn:root/iam/user/local-alice"},
		{Type: policyfeed.UserType, ID: "grn:root/iam/user/local-alice"},
	}, gate.marked[1])
}

// TestOauthCallback_WaitsOnlyForTheSignInThatCreatedTheUser holds that a
// first sign-in waits for the user's entity and, at the bound, redirects
// as after a success with the session it started (ADR-0165 item 4), and
// that a later one, which stored nothing, does not ask the gate: a feed
// that is behind does not hold an existing user out.
//
// The first sign-in, answered by a gate that is clear, is the control
// for the redirect at the bound and for the later one.
func TestOauthCallback_WaitsOnlyForTheSignInThatCreatedTheUser(t *testing.T) {
	f := callbackFixture(t)
	gate := &recordingGate{}
	f.server.WithFeed(gate)
	for _, state := range []string{"state-1", "state-2", "state-3"} {
		require.NoError(t, f.states.Issue(testCtx(t), state, time.Minute, ""))
	}

	rec := f.callback(t, "state-1")
	require.Equal(t, http.StatusFound, rec.Code)
	require.Len(t, gate.marked, 1, "the first sign-in waited")
	assert.Equal(t, []policyfeed.Entity{{Type: policyfeed.UserType, ID: "grn:root/iam/user/123"}}, gate.marked[0])

	gate.err = policyfeed.ErrLinkSetAside
	rec = f.callback(t, "state-2")
	assert.Equal(t, http.StatusFound, rec.Code, "an older mark of the user holds nobody out")
	assert.Len(t, gate.marked, 1, "the gate was not asked")

	other := callbackFixture(t)
	other.provider.identity.Subject = "456"
	gate = &recordingGate{err: policyfeed.ErrNotApplied}
	other.server.WithFeed(gate)
	require.NoError(t, other.states.Issue(testCtx(t), "state-3", time.Minute, ""))
	rec = other.callback(t, "state-3")
	require.Len(t, gate.marked, 1, "it waited")
	require.Equal(t, http.StatusFound, rec.Code, "at the bound the person goes on as after a success")
	assert.NotNil(t, sessionCookieIn(rec), "signed in")
	_, err := other.users.Get(testCtx(t), grn.New("root", user.ServiceType, "456"))
	assert.NoError(t, err, "the user is stored whatever the wait said")
}

// stuckStore holds every mark of every kind: a role link set aside, an
// entity pending. It is the service that applied nothing, whose older
// mark of every kind a request must not wait for.
type stuckStore struct{ policyfeed.Store }

func (stuckStore) LinkState(context.Context, string, string) (policyfeed.LinkState, error) {
	return policyfeed.LinkSetAside, nil
}

func (stuckStore) ScopedLinkState(context.Context, string) (policyfeed.LinkState, error) {
	return policyfeed.LinkSetAside, nil
}

func (stuckStore) EntityPending(context.Context, policyfeed.Entity) (bool, error) {
	return true, nil
}

type nopService struct{ policyfeed.Service }

func (nopService) Wake() {}

// TestRequestsThatRaiseNoMark_AreAnsweredAtOnceWithAnOlderMarkPresent
// holds, per kind of mark, that a request whose write raised none is
// answered at once while the outbox holds an older mark of that kind
// (ADR-0165 item 2): a rename of an organization (Org, OrgMembers),
// of a group (Team, ScopedRoleLink), of a project (Project), of an
// operator (Operator), and the sign-in of a user that exists (User).
//
// The creates beside them are the controls: through the same gate and
// the same older marks they are refused, so the answers above are the
// routes' and not a gate that waits for nothing.
func TestRequestsThatRaiseNoMark_AreAnsweredAtOnceWithAnOlderMarkPresent(t *testing.T) {
	f := callbackFixture(t)
	owner, slug := allPermitted(t, f)
	group := createGroup(t, f, owner, slug, "eng")
	rec := f.do(t, "POST", "/orgs/"+slug+"/projects", `{"name":"web"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	project, err := grn.Parse(decodeBody[apigen.Project](t, rec).Grn)
	require.NoError(t, err)
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.NoError(t, f.users.Create(testCtx(t), user.New(grn.New("root", user.ServiceType, "123"), "Ada")))
	f.server.WithFeed(policyfeed.NewGate(stuckStore{}, nopService{}, 100*time.Millisecond))
	require.NoError(t, f.states.Issue(testCtx(t), "state-older", time.Minute, ""))

	answeredAtOnce := []struct {
		name string
		send func() *httptest.ResponseRecorder
		want int
	}{
		{"rename organization", func() *httptest.ResponseRecorder {
			return f.do(t, "PATCH", "/orgs/"+slug, `{"name":"renamed"}`, owner)
		}, http.StatusOK},
		{"rename group", func() *httptest.ResponseRecorder {
			return f.do(t, "PATCH", "/orgs/"+slug+"/groups/"+group, `{"name":"renamed"}`, owner)
		}, http.StatusOK},
		{"rename project", func() *httptest.ResponseRecorder {
			return f.do(t, "PATCH", "/orgs/"+slug+"/projects/"+project.Identifier, `{"name":"renamed"}`, owner)
		}, http.StatusOK},
		{"rename operator", func() *httptest.ResponseRecorder {
			return f.do(t, "PATCH", "/orgs/"+slug+"/operators/field", `{"name":"renamed"}`, owner)
		}, http.StatusOK},
		{"sign in again", func() *httptest.ResponseRecorder { return f.callback(t, "state-older") }, http.StatusFound},
	}
	for _, c := range answeredAtOnce {
		t.Run(c.name, func(t *testing.T) {
			start := time.Now()
			rec := c.send()
			assert.Equal(t, c.want, rec.Code)
			assert.Less(t, time.Since(start), 90*time.Millisecond, "no wait for the bound")
		})
	}

	rec = f.do(t, "POST", "/orgs/"+slug+"/groups", `{"name":"held"}`, owner)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "a create is held by the same marks")
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"second"}`, owner)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
