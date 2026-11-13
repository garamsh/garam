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
	err   error
	asked []string
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
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "unavailable", body.Kind)
	require.NotNil(t, body.Reason)
	assert.Equal(t, policyfeed.ReasonChangeNotApplied, *body.Reason)
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
