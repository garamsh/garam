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

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
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
	orgs := orgpg.New(p)
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
		authnpg.NewSessionStore(p), nil,
		authnpg.NewPasswordCredentialStore(p),
		authnpg.NewPasswordAttemptStore(p),
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
	orgs    *orgpg.Store
	owner   grn.GRN
}

func newInvitationStack(t *testing.T) *invitationStack {
	t.Helper()
	p := pool(t)
	users := userpg.New(p)
	orgs := orgpg.New(p)
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "owner")
	require.NoError(t, users.Create(context.Background(), user.New(owner, "Owner")))
	srv := api.NewServer(users, orgs, nil, nil, nil, nil,
		authnpg.NewSessionStore(p), nil,
		authnpg.NewPasswordCredentialStore(p),
		authnpg.NewPasswordAttemptStore(p),
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
	sessions := authnpg.NewSessionStore(p)
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
	require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, session, authn.NewSession(bob, time.Hour)))

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

// TestRevokeInvitation_ItsTokenAdmitsNoOneAndAnotherOrgCannotReachIt
// revokes one of two live invitations against the real store: the
// revoked token is refused as unknown by the preview, the accept and
// the registration, while the other stays listed and is still
// accepted, which is the control that the refusals are the revocation's
// doing. Another organization's path does not reach it, and a spent
// invitation is not revoked.
func TestRevokeInvitation_ItsTokenAdmitsNoOneAndAnotherOrgCannotReachIt(t *testing.T) {
	st := newInvitationStack(t)
	ctx := context.Background()
	acme, token := st.invite(t, "acme")
	kept, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, st.orgs.CreateInvitation(ctx,
		org.NewInvitation(kept, acme, org.RoleAdmin, st.owner, time.Now().UTC().Add(time.Hour))))
	stale, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, st.orgs.CreateInvitation(ctx,
		org.NewInvitation(stale, acme, org.RoleAdmin, st.owner, time.Now().UTC().Add(-time.Minute))))
	globex, _ := st.invite(t, "globex")
	bob := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "bob")
	require.NoError(t, st.users.Create(ctx, user.New(bob, "bob")))

	pending, err := st.orgs.ListPendingInvitations(ctx, acme, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, pending, 2, "the expired invitation is not pending")
	byToken := map[string]org.InvitationID{}
	for _, inv := range pending {
		byToken[inv.Token] = inv.ID
	}
	id := byToken[token]
	require.NotZero(t, id)

	assert.ErrorIs(t, st.orgs.RevokeInvitation(ctx, globex, id), org.ErrInvitationNotFound,
		"an id is only the organization's that issued it")
	_, err = st.orgs.GetInvitation(ctx, token)
	require.NoError(t, err, "another organization's revocation deleted nothing")

	require.NoError(t, st.orgs.RevokeInvitation(ctx, acme, id))

	_, err = st.orgs.GetInvitation(ctx, token)
	assert.ErrorIs(t, err, org.ErrInvitationNotFound)
	_, err = st.orgs.AcceptInvitation(ctx, token, bob)
	assert.ErrorIs(t, err, org.ErrInvitationNotFound)
	_, err = st.orgs.ClaimInvitation(ctx, token)
	assert.ErrorIs(t, err, org.ErrInvitationNotFound)
	assert.ErrorIs(t, st.orgs.RevokeInvitation(ctx, acme, id), org.ErrInvitationNotFound)

	pending, err = st.orgs.ListPendingInvitations(ctx, acme, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, kept, pending[0].Token)
	_, err = st.orgs.AcceptInvitation(ctx, kept, bob)
	require.NoError(t, err, "the other invitation still admits")
	assert.ErrorIs(t, st.orgs.RevokeInvitation(ctx, acme, pending[0].ID), org.ErrInvitationAccepted)
	_, err = st.orgs.GetMember(ctx, acme, bob)
	assert.NoError(t, err, "the spent invitation's member stays")
}

// TestListPendingInvitations_PagesTheNewestFirst holds the order the
// specification states, which a page's limit and offset rest on.
func TestListPendingInvitations_PagesTheNewestFirst(t *testing.T) {
	st := newInvitationStack(t)
	ctx := context.Background()
	acme, oldest := st.invite(t, "acme")
	newest, err := org.NewInvitationToken()
	require.NoError(t, err)
	inv := org.NewInvitation(newest, acme, org.RoleMember, st.owner, time.Now().UTC().Add(time.Hour))
	inv.CreatedAt = inv.CreatedAt.Add(time.Minute)
	require.NoError(t, st.orgs.CreateInvitation(ctx, inv))

	first, err := st.orgs.ListPendingInvitations(ctx, acme, listfilter.Filter{Limit: 1})
	require.NoError(t, err)
	second, err := st.orgs.ListPendingInvitations(ctx, acme, listfilter.Filter{Limit: 1, Offset: 1})
	require.NoError(t, err)

	require.Len(t, first, 1)
	require.Len(t, second, 1)
	assert.Equal(t, newest, first[0].Token)
	assert.Equal(t, oldest, second[0].Token)
}

// TestRevokeInvitation_WaitsOnAClaimAndFindsTheInvitationSpent holds a
// claim on the invitation's row in an open transaction, as an accept
// does, and revokes meanwhile: the revocation waits on the row, and
// once the claim commits it is refused as spent and the row stays. A
// revocation and a use never both take effect (ADR-0136).
func TestRevokeInvitation_WaitsOnAClaimAndFindsTheInvitationSpent(t *testing.T) {
	st := newInvitationStack(t)
	ctx := context.Background()
	acme, token := st.invite(t, "acme")
	pending, err := st.orgs.ListPendingInvitations(ctx, acme, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, pending, 1)

	claim, err := st.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = claim.Rollback(ctx) }()
	_, err = claim.Exec(ctx, "UPDATE invitations SET accepted_at = now() WHERE token = $1", token)
	require.NoError(t, err)

	revoked := make(chan error, 1)
	go func() { revoked <- st.orgs.RevokeInvitation(ctx, acme, pending[0].ID) }()
	require.Eventually(t, func() bool {
		return countRows(t, st.pool, `SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE '%DELETE FROM invitations%'`) > 0
	}, 10*time.Second, 10*time.Millisecond, "the revocation waits on the claimed row")

	require.NoError(t, claim.Commit(ctx))

	assert.ErrorIs(t, <-revoked, org.ErrInvitationAccepted)
	assert.Equal(t, 1, countRows(t, st.pool, "SELECT count(*) FROM invitations WHERE token = $1", token))
}
