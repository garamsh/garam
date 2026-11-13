package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	"github.com/garamsh/garam/platform/internal/user"
)

// recordingGate is the wait a request makes for the policy service,
// answering what the test sets and recording what it was asked.
type recordingGate struct {
	err      error
	asked    []string
	entities []policyfeed.Entity
	scoped   []string
}

func (g *recordingGate) AwaitScoped(_ context.Context, owner string) error {
	g.scoped = append(g.scoped, owner)
	return g.err
}

func (g *recordingGate) AwaitEntity(_ context.Context, e policyfeed.Entity) error {
	g.entities = append(g.entities, e)
	return g.err
}

func (g *recordingGate) Await(_ context.Context, org, user string) error {
	g.asked = append(g.asked, org+" "+user)
	return g.err
}

// lastOrg is the organization the gate was last asked about.
func (g *recordingGate) lastOrg(t *testing.T) grn.GRN {
	t.Helper()
	require.NotEmpty(t, g.asked)
	g1, err := grn.Parse(strings.Fields(g.asked[len(g.asked)-1])[0])
	require.NoError(t, err)
	return g1
}

// notApplied reads the answer at the bound: 503, the reason that tells
// a stored change from a service that is down, the organization the
// request stored, and none of the chain's own text.
func notApplied(t *testing.T, rec *httptest.ResponseRecorder, orgGRN grn.GRN) {
	t.Helper()
	storedAnswer(t, rec, orgGRN, policyfeed.ReasonChangeNotApplied)
}

// storedAnswer reads a 503 that carries reason and the organization
// the request stored.
func storedAnswer(t *testing.T, rec *httptest.ResponseRecorder, orgGRN grn.GRN, reasonCode string) {
	t.Helper()
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "unavailable", body.Kind)
	require.NotNil(t, body.Reason)
	assert.Equal(t, reasonCode, *body.Reason)
	require.NotNil(t, body.Resource)
	assert.Equal(t, orgGRN.String(), *body.Resource)
	assert.Equal(t, "internal error", body.Message, "no text of the chain")
}

// TestCreateOrg_AnswersUnavailableWhileTheRoleLinkIsNotApplied holds
// what a caller observes when the policy service has not yet applied
// the owner's role link: 503 with the reason that tells a stored
// change from a service down, naming the organization the request
// created so a client does not create another, and the organization
// stored.
//
// The gate that answers is the control: the same request is 201, so
// the 503 is the wait and not the route.
func TestCreateOrg_AnswersUnavailableWhileTheRoleLinkIsNotApplied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	gate := &recordingGate{}
	f.server.WithFeed(gate)

	rec := f.do(t, "POST", "/orgs", `{"name":"acme"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, "the link is in force")
	require.Len(t, gate.asked, 1, "the answer waited once")

	gate.err = policyfeed.ErrNotApplied
	rec = f.do(t, "POST", "/orgs", `{"name":"globex"}`, owner)

	require.Len(t, gate.asked, 2)
	notApplied(t, rec, gate.lastOrg(t))
	_, err := f.orgs.GetOrg(context.Background(), gate.lastOrg(t))
	assert.NoError(t, err, "the change is stored whatever the wait said")
}

// TestCreateOrg_AnswersChangeRefusedForASetAsideLink holds that a link
// the service refused for good is answered with a reason of its own,
// `change_refused`, and not the bound's `change_not_applied`: the
// first says delivery goes on and the second that it does not. The
// 503 and the stored organization are the same.
//
// The clear gate is the control: the same request is 201, and the
// bound's own answer beside it is `change_not_applied`.
func TestCreateOrg_AnswersChangeRefusedForASetAsideLink(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	gate := &recordingGate{}
	f.server.WithFeed(gate)

	rec := f.do(t, "POST", "/orgs", `{"name":"acme"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, "the link is in force")

	gate.err = policyfeed.ErrNotApplied
	rec = f.do(t, "POST", "/orgs", `{"name":"initech"}`, owner)
	notApplied(t, rec, gate.lastOrg(t))

	gate.err = policyfeed.ErrLinkSetAside
	rec = f.do(t, "POST", "/orgs", `{"name":"globex"}`, owner)

	storedAnswer(t, rec, gate.lastOrg(t), policyfeed.ReasonChangeRefused)
	_, err := f.orgs.GetOrg(context.Background(), gate.lastOrg(t))
	assert.NoError(t, err, "the change is stored whatever the wait said")
}

// TestCreateOrg_WaitsForNothingWithoutAFeed holds that a server with
// no gate answers as it did before the feed existed.
func TestCreateOrg_WaitsForNothingWithoutAFeed(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")

	rec := f.do(t, "POST", "/orgs", `{"name":"acme"}`, owner)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

// TestDeleteOrg_AnswersUnavailableWhileTheRemovalIsNotApplied holds the
// wait of a deletion, which asks about every member of the
// organization (no user). The deleted organization stays deleted.
//
// The clear gate is the control: the same route answers 204.
func TestDeleteOrg_AnswersUnavailableWhileTheRemovalIsNotApplied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	ownerGRN := grn.New("root", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionOrgDelete})
	first := createOrg(t, f, owner, "acme")
	second := createOrg(t, f, owner, "globex")
	gate := &recordingGate{}
	f.server.WithFeed(gate)

	rec := f.do(t, "DELETE", "/orgs/"+first, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code, "the removal is in force")
	require.Equal(t, []string{grn.Org(first).String() + " "}, gate.asked)

	gate.err = policyfeed.ErrNotApplied
	rec = f.do(t, "DELETE", "/orgs/"+second, "", owner)

	notApplied(t, rec, grn.Org(second))
	assert.Equal(t, grn.Org(second).String()+" ", gate.asked[1])
	_, err := f.orgs.GetOrg(context.Background(), grn.Org(second))
	assert.Error(t, err, "the deletion is stored whatever the wait said")
}

// TestAcceptInvitation_AnswersUnavailableWhileTheRoleLinkIsNotApplied
// holds the wait of a join. The invitation is spent and the member
// stored at the bound, so the answer names the organization: a retry
// would meet a spent invitation.
//
// The clear gate is the control: the same route answers 200.
func TestAcceptInvitation_AnswersUnavailableWhileTheRoleLinkIsNotApplied(t *testing.T) {
	f := newFixture(t)
	inviteTo(t, f, "acme", "tok-clear")
	inviteTo(t, f, "globex", "tok-held")
	joiner := f.sessionCookieFor(t, "joiner-sub", "Joiner")
	joinerGRN := grn.New("root", user.ServiceType, "joiner-sub")
	gate := &recordingGate{}
	f.server.WithFeed(gate)

	rec := f.do(t, "POST", "/invitations/tok-clear/accept", "", joiner)
	require.Equal(t, http.StatusOK, rec.Code, "the link is in force")
	require.Equal(t, []string{grn.Org("acme").String() + " " + joinerGRN.String()}, gate.asked)

	gate.err = policyfeed.ErrNotApplied
	rec = f.do(t, "POST", "/invitations/tok-held/accept", "", joiner)

	notApplied(t, rec, grn.Org("globex"))
	_, err := f.orgs.GetMember(context.Background(), grn.Org("globex"), joinerGRN)
	assert.NoError(t, err, "the membership is stored whatever the wait said")
	again := f.do(t, "POST", "/invitations/tok-held/accept", "", joiner)
	require.Equal(t, http.StatusConflict, again.Code, "a retry is refused")
	retry := decodeBody[apigen.Error](t, again)
	require.NotNil(t, retry.Reason)
	assert.Equal(t, org.ReasonInvitationAccepted, *retry.Reason, "the invitation is spent")
}

// TestRegisterPassword_AnswersUnavailableWithTheSessionItStarted holds
// the wait of a registration. At the bound the account, its
// membership and its session exist and the invitation is spent, so
// the answer sets the session cookie and names the organization: the
// person goes on signed in and does not register again.
//
// The clear gate is the control: the same route answers 201.
func TestRegisterPassword_AnswersUnavailableWithTheSessionItStarted(t *testing.T) {
	f := newFixture(t)
	inviteTo(t, f, "acme", "tok-clear")
	inviteTo(t, f, "globex", "tok-held")
	gate := &recordingGate{}
	f.server.WithFeed(gate)

	rec := f.do(t, "POST", "/auth/password/register", registerBody("tok-clear", "alice", registeredPassword))
	require.Equal(t, http.StatusCreated, rec.Code, "the link is in force")
	require.Len(t, gate.asked, 1)

	gate.err = policyfeed.ErrNotApplied
	rec = f.do(t, "POST", "/auth/password/register", registerBody("tok-held", "bob", registeredPassword))

	notApplied(t, rec, grn.Org("globex"))
	cookie := sessionCookie(rec.Result())
	require.NotNil(t, cookie, "the session exists at the bound")
	me := f.do(t, "GET", "/me", "", cookie)
	require.Equal(t, http.StatusOK, me.Code)
	assert.Equal(t, "grn:root/iam/user/local-bob", decodeBody[apigen.User](t, me).Grn)
	_, err := f.orgs.GetMember(context.Background(), grn.Org("globex"),
		grn.New("root", user.ServiceType, "local-bob"))
	assert.NoError(t, err, "the membership is stored whatever the wait said")
}

// TestConsents_AnswerUnavailableWhileTheOrgEntityIsNotApplied holds
// the wait of a grant and of a withdrawal (ADR-0159 item 3): at the
// bound the half is stored whatever the wait said, and the answer is
// 503 with the reason that tells a stored change from a service down,
// naming the organization that made it.
//
// The clear gate is the control: the same routes answer 200 and 204,
// asking the gate about the organization's entity each time.
func TestConsents_AnswerUnavailableWhileTheOrgEntityIsNotApplied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionConsentGrant},
		permission{ownerGRN, org.ActionConsentWithdraw})
	acme := orgGRNForTest(slug)
	globex := orgGRNForTest("globex")
	require.NoError(t, f.orgs.CreateOrg(testCtx(t), org.NewOrganization(globex, "globex")))
	gate := &recordingGate{}
	f.server.WithFeed(gate)
	path := "/orgs/" + slug + "/consents/globex"
	entity := policyfeed.Entity{Type: "Iam::Org", ID: acme.String()}

	rec := f.do(t, "PUT", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code, "the grant is in force")
	rec = f.do(t, "DELETE", path, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code, "the withdrawal is in force")
	require.Equal(t, []policyfeed.Entity{entity, entity}, gate.entities)

	gate.err = policyfeed.ErrNotApplied
	rec = f.do(t, "PUT", path, "", owner)
	storedAnswer(t, rec, acme, policyfeed.ReasonChangeNotApplied)
	listed, err := f.consents.List(testCtx(t), acme)
	require.NoError(t, err)
	require.Len(t, listed, 1, "the half is stored whatever the wait said")

	rec = f.do(t, "DELETE", path, "", owner)
	storedAnswer(t, rec, acme, policyfeed.ReasonChangeNotApplied)
	listed, err = f.consents.List(testCtx(t), acme)
	require.NoError(t, err)
	assert.Empty(t, listed, "the withdrawal is stored whatever the wait said")
}

// TestScopedRoles_AnswerUnavailableWhileTheScopedLinkIsNotApplied holds
// the wait of every request that writes the roles of a group or a
// project or ends one (ADR-0162): each asks the gate about the owner's
// GRN, and at the bound the change is stored whatever the wait said,
// the answer is 503 naming the organization, and a link refused for
// good is answered with its own reason.
//
// The clear gate is the control: the same routes answer as they do
// without a feed, asking the gate once each.
func TestScopedRoles_AnswerUnavailableWhileTheScopedLinkIsNotApplied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionGroupCreate}, permission{ownerGRN, org.ActionGroupUpdate},
		permission{ownerGRN, org.ActionGroupDelete}, permission{ownerGRN, org.ActionProjectCreate},
		permission{ownerGRN, org.ActionProjectUpdate}, permission{ownerGRN, org.ActionProjectDelete},
		permission{ownerGRN, org.ActionGroupRead}, permission{ownerGRN, org.ActionProjectRead})
	gate := &recordingGate{}
	f.server.WithFeed(gate)
	acme := orgGRNForTest(slug)
	base := "/orgs/" + slug

	for _, kind := range []string{"groups", "projects"} {
		t.Run(kind, func(t *testing.T) {
			gate.scoped = nil
			gate.err = nil
			rec := f.do(t, "POST", base+"/"+kind, `{"name":"x","roles":["admin"]}`, owner)
			require.Equal(t, http.StatusCreated, rec.Code, "the link is in force")
			created, err := grn.Parse(decodeBody[apigen.Group](t, rec).Grn)
			require.NoError(t, err)
			path := base + "/" + kind + "/" + created.Identifier
			rec = f.do(t, "PATCH", path, `{"roles":["member"]}`, owner)
			require.Equal(t, http.StatusOK, rec.Code, "the link is in force")
			rec = f.do(t, "PATCH", path, `{"name":"y"}`, owner)
			require.Equal(t, http.StatusOK, rec.Code, "the link is in force")
			require.Equal(t, []string{created.String(), created.String(), created.String()}, gate.scoped)

			gate.err = policyfeed.ErrNotApplied
			rec = f.do(t, "PATCH", path, `{"roles":["owner"]}`, owner)
			notApplied(t, rec, acme)
			rec = f.do(t, "POST", base+"/"+kind, `{"name":"z"}`, owner)
			notApplied(t, rec, acme)
			gate.err = policyfeed.ErrLinkSetAside
			rec = f.do(t, "DELETE", path, "", owner)
			storedAnswer(t, rec, acme, policyfeed.ReasonChangeRefused)
			gate.err = nil
			rec = f.do(t, "GET", path, "", owner)
			assert.Equal(t, http.StatusNotFound, rec.Code, "the deletion is stored whatever the wait said")
		})
	}
}
