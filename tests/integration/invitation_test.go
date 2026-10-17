package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

// TestGetInvitation_ReadsEachStateFromPostgres reads one invitation
// before and after a registration spends it, and a second one that
// expired unused, against the real store: whether an invitation was
// accepted is the accepted_at column, which only the store writes. The
// pending read before the registration is the control that the
// accepted state is the registration's doing.
func TestGetInvitation_ReadsEachStateFromPostgres(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	users := userpg.New(p)
	orgs := org.NewPostgresStore(p)
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "owner")
	require.NoError(t, users.Create(ctx, user.New(owner, "Owner")))
	acme := grn.Org("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "Acme Robotics")))
	live, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, orgs.CreateInvitation(ctx,
		org.NewInvitation(live, acme, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))
	stale, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, orgs.CreateInvitation(ctx,
		org.NewInvitation(stale, acme, org.RoleAdmin, owner, time.Now().UTC().Add(-time.Minute))))

	srv := api.NewServer(users, orgs, nil, nil, nil, nil,
		authn.NewPostgresSessionStore(p), nil,
		authn.NewPostgresPasswordCredentialStore(p),
		authn.NewPostgresPasswordAttemptStore(p),
		nil, nil, nil, nil, nil, nil, api.Config{SessionTTL: time.Hour})
	handler := apigen.Handler(srv)
	read := func(token string) (int, apigen.InvitationPreview) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invitations/"+token, nil))
		var preview apigen.InvitationPreview
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
		}
		return rec.Code, preview
	}

	status, before := read(live)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, apigen.InvitationPreviewStatePending, before.State)
	assert.Equal(t, "acme", before.Org)
	assert.Equal(t, "Acme Robotics", before.OrgName)
	assert.Equal(t, apigen.RoleMember, before.Role)

	req := httptest.NewRequest(http.MethodPost, "/auth/password/register", strings.NewReader(
		fmt.Sprintf(`{"invitationToken":%q,"username":"alice","password":%q}`, live, integrationPassword)))
	req.Header.Set("Content-Type", "application/json")
	registered := httptest.NewRecorder()
	handler.ServeHTTP(registered, req)
	require.Equal(t, http.StatusCreated, registered.Code, registered.Body.String())

	status, after := read(live)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, apigen.InvitationPreviewStateAccepted, after.State)

	status, expired := read(stale)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, apigen.InvitationPreviewStateExpired, expired.State)
	assert.Equal(t, apigen.RoleAdmin, expired.Role)

	status, _ = read("no-such-token")
	assert.Equal(t, http.StatusNotFound, status)
}

// raceRounds is how many invitations each race below spends. One round
// can order the two requests so the second starts after the first
// finished, which the unfixed code survives; many rounds make an
// overlapping pair all but certain.
const raceRounds = 12

// race serves both requests at once, released together, and returns
// their responses in the order given.
func race(handler http.Handler, a, b *http.Request) (*httptest.ResponseRecorder, *httptest.ResponseRecorder) {
	recs := [2]*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, req := range []*http.Request{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			handler.ServeHTTP(recs[i], req)
		}()
	}
	close(start)
	wg.Wait()
	return recs[0], recs[1]
}

// invitationStack is a server over the real stores with an owner who
// issues invitations.
type invitationStack struct {
	pool    *pgxpool.Pool
	handler http.Handler
	users   *userpg.Store
	orgs    *org.PostgresStore
	owner   grn.GRN
}

func newInvitationStack(t *testing.T) *invitationStack {
	t.Helper()
	p := pool(t)
	users := userpg.New(p)
	orgs := org.NewPostgresStore(p)
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "owner")
	require.NoError(t, users.Create(context.Background(), user.New(owner, "Owner")))
	srv := api.NewServer(users, orgs, nil, nil, nil, nil,
		authn.NewPostgresSessionStore(p), nil,
		authn.NewPostgresPasswordCredentialStore(p),
		authn.NewPostgresPasswordAttemptStore(p),
		nil, nil, nil, nil, nil, nil, api.Config{SessionTTL: time.Hour})
	return &invitationStack{pool: p, handler: apigen.Handler(srv), users: users, orgs: orgs, owner: owner}
}

// invite creates an org of its own and a live invitation into it.
func (st *invitationStack) invite(t *testing.T, slug string) (grn.GRN, string) {
	t.Helper()
	ctx := context.Background()
	g := grn.Org(slug)
	require.NoError(t, st.orgs.CreateOrg(ctx, org.NewOrganization(g, slug)))
	token, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, st.orgs.CreateInvitation(ctx,
		org.NewInvitation(token, g, org.RoleMember, st.owner, time.Now().UTC().Add(time.Hour))))
	return g, token
}

// winner names which of two raced responses carried want, failing
// unless exactly one did and the other was refused as spent.
func winner(t *testing.T, want int, a, b *httptest.ResponseRecorder) int {
	t.Helper()
	codes := []int{a.Code, b.Code}
	switch {
	case a.Code == want && b.Code == http.StatusConflict:
		return 0
	case b.Code == want && a.Code == http.StatusConflict:
		return 1
	}
	t.Fatalf("want one %d and one 409, got %v (%s | %s)", want, codes, a.Body, b.Body)
	return -1
}

// TestAcceptInvitation_TwoRacingAcceptsJoinOne races two signed-in
// users accepting one invitation: exactly one joins, the other is
// refused, and the invitation names the one who joined (#1381).
func TestAcceptInvitation_TwoRacingAcceptsJoinOne(t *testing.T) {
	st := newInvitationStack(t)
	p := st.pool
	ctx := context.Background()
	sessions := authn.NewPostgresSessionStore(p)
	var people []grn.GRN
	var cookies []*http.Cookie
	for _, id := range []string{"bob", "carol"} {
		g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
		require.NoError(t, st.users.Create(ctx, user.New(g, id)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, sessions.Create(ctx, token, authn.NewSession(g, time.Hour)))
		people = append(people, g)
		cookies = append(cookies, &http.Cookie{Name: "garam_session", Value: token})
	}
	accept := func(token string, c *http.Cookie) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/invitations/"+token+"/accept", nil)
		req.AddCookie(c)
		return req
	}

	for round := range raceRounds {
		orgGRN, token := st.invite(t, fmt.Sprintf("acme-%d", round))
		a, b := race(st.handler, accept(token, cookies[0]), accept(token, cookies[1]))
		won := winner(t, http.StatusOK, a, b)

		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM org_members WHERE org_grn = $1", orgGRN.String()),
			"round %d: one member joined", round)
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM org_members WHERE org_grn = $1 AND user_grn = $2",
			orgGRN.String(), people[won].String()), "round %d: the one who was answered", round)
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM invitations WHERE token = $1 AND accepted_by = $2",
			token, people[won].String()), "round %d: and the invitation names them", round)
	}
}

// TestRegisterPassword_TwoRacingRegistrationsCreateOneAccount races two
// registrations under different usernames on one invitation: exactly
// one account is created, with its credential and membership, and the
// other request creates nothing at all (#1381).
func TestRegisterPassword_TwoRacingRegistrationsCreateOneAccount(t *testing.T) {
	st := newInvitationStack(t)
	p := st.pool
	register := func(token, username string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/auth/password/register", strings.NewReader(
			fmt.Sprintf(`{"invitationToken":%q,"username":%q,"password":%q}`, token, username, integrationPassword)))
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	for round := range raceRounds {
		orgGRN, token := st.invite(t, fmt.Sprintf("acme-%d", round))
		names := []string{fmt.Sprintf("alice-%d", round), fmt.Sprintf("carol-%d", round)}
		accounts := []string{
			"grn:root:default:user:local-" + names[0],
			"grn:root:default:user:local-" + names[1],
		}
		a, b := race(st.handler, register(token, names[0]), register(token, names[1]))
		won := winner(t, http.StatusCreated, a, b)
		lost := 1 - won

		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM users WHERE grn = $1", accounts[won]), "round %d: the account", round)
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM password_credentials WHERE user_grn = $1", accounts[won]),
			"round %d: its credential", round)
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM org_members WHERE org_grn = $1", orgGRN.String()),
			"round %d: one member", round)
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM invitations WHERE token = $1 AND accepted_by = $2",
			token, accounts[won]), "round %d: the invitation names it", round)
		assert.Equal(t, 0, countRows(t, p,
			"SELECT count(*) FROM users WHERE grn = $1", accounts[lost]),
			"round %d: the refused registration created no user", round)
	}
}

// TestInvitation_AnAcceptRacingARegistrationAdmitsOne races a signed-in
// user's accept against a registration on one invitation: one of the
// two is admitted, the other is refused as spent, and the org gains
// exactly one member (#1381).
func TestInvitation_AnAcceptRacingARegistrationAdmitsOne(t *testing.T) {
	st := newInvitationStack(t)
	p := st.pool
	ctx := context.Background()
	bob := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "bob")
	require.NoError(t, st.users.Create(ctx, user.New(bob, "bob")))
	session, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, authn.NewPostgresSessionStore(p).Create(ctx, session, authn.NewSession(bob, time.Hour)))

	for round := range raceRounds {
		orgGRN, token := st.invite(t, fmt.Sprintf("acme-%d", round))
		accept := httptest.NewRequest(http.MethodPost, "/invitations/"+token+"/accept", nil)
		accept.AddCookie(&http.Cookie{Name: "garam_session", Value: session})
		account := fmt.Sprintf("grn:root:default:user:local-dave-%d", round)
		register := httptest.NewRequest(http.MethodPost, "/auth/password/register", strings.NewReader(
			fmt.Sprintf(`{"invitationToken":%q,"username":"dave-%d","password":%q}`, token, round, integrationPassword)))
		register.Header.Set("Content-Type", "application/json")

		a, r := race(st.handler, accept, register)
		admitted := bob.String()
		switch {
		case a.Code == http.StatusOK && r.Code == http.StatusConflict:
		case r.Code == http.StatusCreated && a.Code == http.StatusConflict:
			admitted = account
		default:
			t.Fatalf("round %d: want one admitted and one 409, got accept %d, register %d (%s | %s)",
				round, a.Code, r.Code, a.Body, r.Body)
		}
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM org_members WHERE org_grn = $1", orgGRN.String()),
			"round %d: one member", round)
		assert.Equal(t, 1, countRows(t, p,
			"SELECT count(*) FROM invitations WHERE token = $1 AND accepted_by = $2", token, admitted),
			"round %d: the invitation names who was admitted", round)
		if admitted == bob.String() {
			assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM users WHERE grn = $1", account),
				"round %d: the refused registration created no user", round)
		}
	}
}
