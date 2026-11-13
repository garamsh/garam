package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/policyfeed"
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

// TestCreateOrg_AnswersUnavailableWhileTheRoleLinkIsNotApplied holds
// what a caller observes when the policy service has not yet applied
// the owner's role link: 503 with the reason that tells a stored
// change from a service that is down, and the organization stored.
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

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "unavailable", body.Kind)
	require.NotNil(t, body.Reason)
	assert.Equal(t, policyfeed.ReasonChangeNotApplied, *body.Reason)
	assert.Equal(t, "internal error", body.Message, "no text of the chain")
	require.Len(t, gate.asked, 2)
	g, err := grn.Parse(strings.Fields(gate.asked[1])[0])
	require.NoError(t, err)
	_, err = f.orgs.GetOrg(context.Background(), g)
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
