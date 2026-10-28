package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// The password these tests register with. It is a fixture of this file
// and no account outside it is created with one.
const integrationPassword = "correct-horse-battery-staple"

// TestPasswordCredentialStore_RoundTrip exercises the migration, the
// generated queries and the foreign key together: what the store
// writes is what it reads back, and replacing a verifier — the
// parameter upgrade's write — leaves one row rather than two.
func TestPasswordCredentialStore_RoundTrip(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	users := userpg.New(p)
	alice := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "local-alice")
	require.NoError(t, users.Create(ctx, user.New(alice, "alice")))

	credentials := authnpg.NewPasswordCredentialStore(p)
	_, err := credentials.Get(ctx, alice)
	require.ErrorIs(t, err, authn.ErrPasswordCredentialNotFound, "absent before it is written")

	first, err := authn.NewVerifier(integrationPassword)
	require.NoError(t, err)
	require.NoError(t, credentials.Create(ctx, alice, first))

	held, err := credentials.Get(ctx, alice)
	require.NoError(t, err)
	assert.Equal(t, first, held)
	assert.NoError(t, held.Match(integrationPassword))

	second, err := authn.NewVerifier(integrationPassword)
	require.NoError(t, err)
	require.NoError(t, credentials.Replace(ctx, alice, second))
	held, err = credentials.Get(ctx, alice)
	require.NoError(t, err)
	assert.Equal(t, second, held)
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM password_credentials"),
		"replacing writes over the row rather than beside it")

	// The credential belongs to the user: deleting the user takes it.
	require.NoError(t, users.Delete(ctx, alice))
	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM password_credentials"))
}

// TestPasswordCredentialStore_ClassifiesWhatTheSchemaRefuses covers the
// two refusals the table carries (000016): one credential per user, and
// a credential only for a user that exists. Each reaches the caller as
// its own kind rather than as the chain's default, which is what the
// store reading its driver errors through pgxerr buys.
//
// The write that lands is the control: the same call, with the same
// verifier, succeeds for a user holding no credential — so what the two
// refusals name is the constraint rather than a store refusing
// everything.
func TestPasswordCredentialStore_ClassifiesWhatTheSchemaRefuses(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	users := userpg.New(p)
	alice := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "local-alice")
	require.NoError(t, users.Create(ctx, user.New(alice, "alice")))

	verifier, err := authn.NewVerifier(integrationPassword)
	require.NoError(t, err)
	credentials := authnpg.NewPasswordCredentialStore(p)
	require.NoError(t, credentials.Create(ctx, alice, verifier), "a first credential for a real user lands")

	// 23505 on password_credentials' primary key: the user holds one.
	err = credentials.Create(ctx, alice, verifier)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindAlreadyExists),
		"a second credential for one user is a conflict, not an outage")

	// 23503 on its reference to users: there is nobody to hold it.
	ghost := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "local-ghost")
	err = credentials.Create(ctx, ghost, verifier)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound),
		"a credential for a user that does not exist names the missing row")
}

// TestPasswordAttemptStore_BoundsWhatIsLive drives the statement that
// enforces the bound, against the database that enforces it. ADR-0030
// put the decision in the writing statement so it holds across every
// replica; ADR-0041 applies the same shape to the Argon2id
// computation, and this is the SQL doing it.
//
// Releasing a slot and recording once more is the control: it shows the
// refusal tracks the attempts still live rather than the number of
// calls made, which would refuse the last one too.
func TestPasswordAttemptStore_BoundsWhatIsLive(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	attempts := authnpg.NewPasswordAttemptStore(p)
	for i := range authn.MaxLivePasswordAttempts {
		require.NoError(t, attempts.Record(ctx, fmt.Sprintf("attempt-%d", i), time.Hour))
	}
	assert.Equal(t, authn.MaxLivePasswordAttempts,
		countRows(t, p, "SELECT count(*) FROM password_attempts"))

	err := attempts.Record(ctx, "one-too-many", time.Hour)
	assert.ErrorIs(t, err, authn.ErrPasswordAttemptRateLimited)
	assert.True(t, errorx.IsKind(err, errorx.KindResourceExhausted))
	assert.Equal(t, authn.MaxLivePasswordAttempts,
		countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"the refused attempt wrote nothing")

	require.NoError(t, attempts.Release(ctx, "attempt-0"))
	assert.NoError(t, attempts.Record(ctx, "one-more", time.Hour))
}

// TestPasswordAttemptStore_PrunesWhatLapsed holds the property that
// keeps the table from wedging: an attempt past its lifetime frees its
// slot without anybody releasing it, and the prune runs on the write
// that is itself refused.
func TestPasswordAttemptStore_PrunesWhatLapsed(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	attempts := authnpg.NewPasswordAttemptStore(p)
	for i := range authn.MaxLivePasswordAttempts {
		require.NoError(t, attempts.Record(ctx, fmt.Sprintf("lapsed-%d", i), -time.Second))
	}
	// Every row is already past its lifetime, so none of them is live
	// and the next record both sweeps them and is accepted.
	require.NoError(t, attempts.Record(ctx, "after-the-sweep", time.Hour))
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"the sweep left only the attempt that is live")
}

// TestRegisterPassword_ADuplicateCarriesNoneOfPostgresText is the
// disclosure regression against the real driver: registering one
// username twice reaches users.Create with the row present, and what
// Postgres says about its own primary key must not reach a caller who
// holds no credential at all — this route is unauthenticated.
//
// The first registration is the control: same route, same body shape,
// 201, so the refusal is the second row rather than the route. What
// the assertions turn on is what the body does not carry; a 409 with
// the right kind and a valid body is what the defect already produced.
func TestRegisterPassword_ADuplicateCarriesNoneOfPostgresText(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	users := userpg.New(p)
	orgs := orgpg.New(p)
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "owner")
	require.NoError(t, users.Create(ctx, user.New(owner, "Owner")))
	acme := grn.Org("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	// Two live invitations, because one is spent by the registration
	// that lands and the second call has to reach the user row.
	tokens := make([]string, 2)
	for i := range tokens {
		token, err := org.NewInvitationToken()
		require.NoError(t, err)
		require.NoError(t, orgs.CreateInvitation(ctx,
			org.NewInvitation(token, acme, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))
		tokens[i] = token
	}

	srv := api.NewServer(users, orgs, nil, nil, nil, nil,
		authnpg.NewSessionStore(p), nil,
		authnpg.NewPasswordCredentialStore(p),
		authnpg.NewPasswordAttemptStore(p),
		nil, nil, nil, nil, nil, nil, api.Config{SessionTTL: time.Hour})
	handler := apigen.Handler(srv)
	register := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/password/register", strings.NewReader(
			fmt.Sprintf(`{"invitationToken":%q,"username":"alice","password":%q}`, token, integrationPassword)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusCreated, register(tokens[0]).Code, "the control: the first registration lands")

	rec := register(tokens[1])
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	// The control for the search: the refusal carries a message, so
	// what is absent below is the schema and not the whole body.
	require.Contains(t, rec.Body.String(), `"message"`)
	for _, leaked := range []string{"users_pkey", "SQLSTATE", "23505", "duplicate key", "constraint"} {
		assert.NotContains(t, rec.Body.String(), leaked,
			"the response names something only Postgres and the schema know")
	}
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM users WHERE grn = $1",
		"grn:root:default:user:local-alice"), "the refused registration wrote no second row")
}

// TestPasswordSignIn_RoundTripAgainstPostgres walks the whole route
// against the real database: an invitation becomes an account, a
// credential, a membership and a session, and the password signs in
// again afterwards. A wrong password and an unknown username are
// refused identically here as they are in the handler's own tests —
// asserted again because this is the path with the real store behind
// it (ADR-0041).
func TestPasswordSignIn_RoundTripAgainstPostgres(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	users := userpg.New(p)
	orgs := orgpg.New(p)
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "owner")
	require.NoError(t, users.Create(ctx, user.New(owner, "Owner")))
	acme := grn.Org("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	token, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, orgs.CreateInvitation(ctx,
		org.NewInvitation(token, acme, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))

	srv := api.NewServer(users, orgs, nil, nil, nil, nil,
		authnpg.NewSessionStore(p), nil,
		authnpg.NewPasswordCredentialStore(p),
		authnpg.NewPasswordAttemptStore(p),
		nil, nil, nil, nil, nil, nil, api.Config{SessionTTL: time.Hour})
	handler := apigen.Handler(srv)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := post("/auth/password/register",
		fmt.Sprintf(`{"invitationToken":%q,"username":"alice","password":%q}`, token, integrationPassword))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM password_credentials"))
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM org_members WHERE user_grn = $1", "grn:root:default:user:local-alice"))
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1 AND accepted_at IS NOT NULL", token))

	accepted := post("/auth/password/login", fmt.Sprintf(`{"username":"alice","password":%q}`, integrationPassword))
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"a verification that succeeded holds no slot")

	// The two refusals, against the real store: one status, one body.
	wrongPassword := post("/auth/password/login", `{"username":"alice","password":"wrong-password-entirely"}`)
	unknownUser := post("/auth/password/login", `{"username":"mallory","password":"wrong-password-entirely"}`)
	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
	assert.Equal(t, unknownUser.Code, wrongPassword.Code)
	assert.Equal(t, unknownUser.Body.String(), wrongPassword.Body.String())
	// The control for the search below: the refusal carries a message,
	// so what is absent is the password and not the whole body.
	require.Contains(t, wrongPassword.Body.String(), "message")
	assert.NotContains(t, wrongPassword.Body.String(), "wrong-password-entirely")
	assert.Equal(t, 2, countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"and each failure keeps its slot")
}
