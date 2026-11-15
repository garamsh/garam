package http_test

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/platform"
	platformhttp "github.com/garamsh/garam/cowork/internal/platform/http"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// fakePlatform answers `me` for the session "ada" and the bearer
// "cli-ada", and the organization list.
type fakePlatform struct{}

func (f *fakePlatform) caller(r *nethttp.Request) string {
	if r.Header.Get("Authorization") == "Bearer cli-ada" {
		return "ada"
	}
	if c, err := r.Cookie("garam_session"); err == nil && c.Value == "ada" && r.Header.Get("Authorization") == "" {
		return "ada"
	}
	return ""
}

func (f *fakePlatform) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
	who := f.caller(r)
	if who == "" {
		w.WriteHeader(nethttp.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/me":
		_ = json.NewEncoder(w).Encode(map[string]string{"grn": grn.New(grn.RootOwner, "user", who).String(), "nickname": who})
	case "/orgs":
		_ = json.NewEncoder(w).Encode([]map[string]string{{"grn": "grn:root/iam/org/acme", "name": "Acme"}})
	default:
		w.WriteHeader(nethttp.StatusInternalServerError)
	}
}

func newClient(t *testing.T, f *fakePlatform) *platformhttp.Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := platformhttp.New(srv.URL)
	require.NoError(t, err)
	return c
}

// TestIdentify_PresentsTheCallersCredential holds that the cookie and
// the bearer each reach the platform as the caller presented them,
// and that a credential the platform refuses is unauthenticated.
func TestIdentify_PresentsTheCallersCredential(t *testing.T) {
	c := newClient(t, &fakePlatform{})
	for name, cred := range map[string]platform.Credential{
		"session": {Session: "ada"},
		"bearer":  {Bearer: "cli-ada"},
	} {
		t.Run(name, func(t *testing.T) {
			caller, err := c.Identify(context.Background(), cred)
			require.NoError(t, err)
			assert.Equal(t, "grn:root/iam/user/ada", caller.GRN.String())
			assert.Equal(t, cred, caller.Credential)
		})
	}

	_, err := c.Identify(context.Background(), platform.Credential{Session: "eve"})
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
	_, err = c.Identify(context.Background(), platform.Credential{})
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
}

// TestIdentify_AnUnreachableOrFailingPlatformIsUnavailable holds that
// a platform that answers an error, or cannot be reached, is
// unavailable rather than a refusal of the credential. The session
// the platform accepts is the control.
func TestIdentify_AnUnreachableOrFailingPlatformIsUnavailable(t *testing.T) {
	c := newClient(t, &fakePlatform{})
	_, err := c.Identify(context.Background(), platform.Credential{Session: "ada"})
	require.NoError(t, err, "control")

	gone, err := platformhttp.New("http://127.0.0.1:1")
	require.NoError(t, err)
	_, err = gone.Identify(context.Background(), platform.Credential{Session: "ada"})
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "%v", err)
}

// TestOrganizations_AnswersTheCallersByIdentifier reads acme back by
// the last segment of its GRN, as cowork's paths name it.
func TestOrganizations_AnswersTheCallersByIdentifier(t *testing.T) {
	c := newClient(t, &fakePlatform{})
	orgs, err := c.Organizations(context.Background(), platform.Caller{Credential: platform.Credential{Session: "ada"}})
	require.NoError(t, err)
	assert.Equal(t, []platform.Organization{{ID: "acme", Name: "Acme"}}, orgs)
}
