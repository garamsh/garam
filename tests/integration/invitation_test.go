package integration_test

import (
	"context"
	"encoding/json"
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
	acme := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, "acme")
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
