package e2e_test

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

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// The password these tests register with. It is a fixture of this file
// and no account outside it is created with one.
const e2ePassword = "correct-horse-battery-staple"

// TestPasswordCredentialStore_RoundTrip exercises the migration, the
// generated queries and the foreign key together: what the store
// writes is what it reads back, and replacing a verifier — the
// parameter upgrade's write — leaves one row rather than two.
func TestPasswordCredentialStore_RoundTrip(t *testing.T) {
	p := pool(t)
	truncate(t, p)
	ctx := context.Background()

	users := user.NewPostgresStore(p)
	alice := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "local-alice")
	require.NoError(t, users.Create(ctx, user.New(alice, "alice")))

	credentials := authn.NewPostgresPasswordCredentialStore(p)
	_, err := credentials.Get(ctx, alice)
	require.ErrorIs(t, err, authn.ErrPasswordCredentialNotFound, "absent before it is written")

	first, err := authn.NewVerifier(e2ePassword)
	require.NoError(t, err)
	require.NoError(t, credentials.Create(ctx, alice, first))

	held, err := credentials.Get(ctx, alice)
	require.NoError(t, err)
	assert.Equal(t, first, held)
	assert.NoError(t, held.Match(e2ePassword))

	second, err := authn.NewVerifier(e2ePassword)
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

	attempts := authn.NewPostgresPasswordAttemptStore(p)
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

	attempts := authn.NewPostgresPasswordAttemptStore(p)
	for i := range authn.MaxLivePasswordAttempts {
		require.NoError(t, attempts.Record(ctx, fmt.Sprintf("lapsed-%d", i), -time.Second))
	}
	// Every row is already past its lifetime, so none of them is live
	// and the next record both sweeps them and is accepted.
	require.NoError(t, attempts.Record(ctx, "after-the-sweep", time.Hour))
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"the sweep left only the attempt that is live")
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

	users := user.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "owner")
	require.NoError(t, users.Create(ctx, user.New(owner, "Owner")))
	acme := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, "acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	token, err := org.NewInvitationToken()
	require.NoError(t, err)
	require.NoError(t, orgs.CreateInvitation(ctx,
		org.NewInvitation(token, acme, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))

	srv := api.NewServer(users, orgs, nil, nil, nil, nil,
		authn.NewPostgresSessionStore(p), nil,
		authn.NewPostgresPasswordCredentialStore(p),
		authn.NewPostgresPasswordAttemptStore(p),
		nil, nil, nil, api.Config{SessionTTL: time.Hour})
	handler := apigen.Handler(srv)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := post("/auth/password/register",
		fmt.Sprintf(`{"invitationToken":%q,"username":"alice","password":%q}`, token, e2ePassword))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM password_credentials"))
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM org_members WHERE user_grn = $1", "grn:root:default:user:local-alice"))
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1 AND accepted_at IS NOT NULL", token))

	accepted := post("/auth/password/login", fmt.Sprintf(`{"username":"alice","password":%q}`, e2ePassword))
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"a verification that succeeded holds no slot")

	// The two refusals, against the real store: one status, one body.
	wrongPassword := post("/auth/password/login", `{"username":"alice","password":"wrong-password-entirely"}`)
	unknownUser := post("/auth/password/login", `{"username":"mallory","password":"wrong-password-entirely"}`)
	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
	assert.Equal(t, unknownUser.Code, wrongPassword.Code)
	assert.Equal(t, unknownUser.Body.String(), wrongPassword.Body.String())
	assert.NotContains(t, wrongPassword.Body.String(), "wrong-password-entirely")
	assert.Equal(t, 2, countRows(t, p, "SELECT count(*) FROM password_attempts"),
		"and each failure keeps its slot")
}
