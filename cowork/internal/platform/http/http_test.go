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
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// fakePlatform answers `me` for the session "ada" and the bearer
// "cli-ada", and permission checks for ada in acme, allowing what
// allowed names. It records the last check request's body.
type fakePlatform struct {
	allowed map[string]bool
	asked   []map[string]any
}

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
		_ = json.NewEncoder(w).Encode(map[string]string{"grn": "grn:root:default:user:" + who, "nickname": who})
	case "/orgs/acme/permission-checks":
		var body struct {
			Checks []map[string]any `json:"checks"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.asked = body.Checks
		results := make([]map[string]string, len(body.Checks))
		for i, c := range body.Checks {
			key := c["action"].(string)
			if res, ok := c["resource"].(map[string]any); ok {
				key += "@" + res["type"].(string) + "/" + res["id"].(string)
			}
			decision := "deny"
			if f.allowed[key] {
				decision = "allow"
			}
			results[i] = map[string]string{"decision": decision}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case "/orgs":
		_ = json.NewEncoder(w).Encode([]map[string]string{{"grn": "grn:root:default:org:acme", "name": "Acme"}})
	case "/orgs/globex/permission-checks":
		w.WriteHeader(nethttp.StatusForbidden)
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
			assert.Equal(t, "grn:root:default:user:ada", caller.GRN.String())
			assert.Equal(t, cred, caller.Credential)
		})
	}

	_, err := c.Identify(context.Background(), platform.Credential{Session: "eve"})
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
	_, err = c.Identify(context.Background(), platform.Credential{})
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
}

// TestAllowed_AnswersEachCheckInOrder asks a write on one page the
// platform allows, the same write on another, and a read of the page
// tree, and reads back allow, deny, allow. It also holds that a page
// is asked as a cowork-page resource and the tree as no resource.
func TestAllowed_AnswersEachCheckInOrder(t *testing.T) {
	f := &fakePlatform{allowed: map[string]bool{
		"cowork:page-write@cowork-page/p-1": true,
		"cowork:page-read":                  true,
	}}
	c := newClient(t, f)
	caller, err := c.Identify(context.Background(), platform.Credential{Session: "ada"})
	require.NoError(t, err)

	got, err := c.Allowed(context.Background(), caller, "acme", []platform.Check{
		platform.OnPage(platform.ActionPageWrite, "p-1"),
		platform.OnPage(platform.ActionPageWrite, "p-2"),
		{Action: platform.ActionPageRead},
	})
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false, true}, got)
	require.Len(t, f.asked, 3)
	assert.NotContains(t, f.asked[2], "resource")
}

// TestAllowed_KeepsThePlatformsRefusals maps a non-member's 403 to
// permission denied and an error the platform answers, or a platform
// that cannot be reached, to unavailable: cowork refuses on either
// rather than reading a missing answer as allow. The member's answer
// in acme is the control.
func TestAllowed_KeepsThePlatformsRefusals(t *testing.T) {
	f := &fakePlatform{allowed: map[string]bool{"cowork:page-read": true}}
	c := newClient(t, f)
	caller := platform.Caller{Credential: platform.Credential{Session: "ada"}}
	read := []platform.Check{{Action: platform.ActionPageRead}}

	got, err := c.Allowed(context.Background(), caller, "acme", read)
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, got)

	_, err = c.Allowed(context.Background(), caller, "globex", read)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
	_, err = c.Allowed(context.Background(), caller, "initech", read)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "%v", err)

	gone, err := platformhttp.New("http://127.0.0.1:1")
	require.NoError(t, err)
	_, err = gone.Allowed(context.Background(), caller, "acme", read)
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

// TestRequire_RefusesWhenAnyCheckIsDenied holds that one denied check
// refuses the whole, and all allowed passes.
func TestRequire_RefusesWhenAnyCheckIsDenied(t *testing.T) {
	f := &fakePlatform{allowed: map[string]bool{"cowork:page-read": true}}
	c := newClient(t, f)
	caller := platform.Caller{Credential: platform.Credential{Session: "ada"}}

	require.NoError(t, platform.Require(context.Background(), c, caller, "acme", platform.Check{Action: platform.ActionPageRead}))
	err := platform.Require(context.Background(), c, caller, "acme",
		platform.Check{Action: platform.ActionPageRead}, platform.Check{Action: platform.ActionPageWrite})
	assert.ErrorIs(t, err, platform.ErrNotAllowed)
}
