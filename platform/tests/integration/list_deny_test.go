package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apigen "github.com/garamsh/garam/platform/api"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/grn"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// listStack is the browser-facing listener over real Postgres and a
// real policy engine, with the stores every org-gated list reads. acme
// holds two owners: ada, whom a test denies one object, and dan, the
// control the Deny does not reach.
type listStack struct {
	url            string
	pool           *pgxpool.Pool
	acme           grn.GRN
	policies       *authz.PolicyGRPCServer
	orgs           *orgpg.Store
	agents         *agentpg.Store
	ada, dan       *http.Cookie
	adaGRN, danGRN grn.GRN
}

func newListStack(t *testing.T) *listStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	session := func(id string) (*http.Cookie, grn.GRN) {
		g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, org.RoleOwner)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}, g
	}
	s := &listStack{pool: p, acme: acme, policies: policies, orgs: orgs, agents: agentpg.New(p)}
	s.ada, s.adaGRN = session("ada")
	s.dan, s.danGRN = session("dan")

	srv := api.NewServer(users, orgs, s.agents, nil, messagepg.New(p), nil,
		authnpg.NewSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), api.NewGRPCPolicyManager(policyService(t, policies)), nil, cli.NewAuthorityService(p, engine), nil,
		api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	s.url = browser.URL
	return s
}

// policyService serves policies' lifecycle service, the one
// listPolicies reads, and answers a connection to it. It is the same
// server activePolicy and attachPolicy write through, so the list reads
// what a test wrote.
func policyService(t *testing.T, policies *authz.PolicyGRPCServer) *grpc.ClientConn {
	t.Helper()
	srv := grpc.NewServer()
	authzv1.RegisterPolicyServiceServer(srv, policies)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// get makes one GET as cookie and answers the status and body.
func (s *listStack) get(t *testing.T, path string, query url.Values, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.url+path+"?"+query.Encode(), nil)
	require.NoError(t, err)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// listed lists path as cookie with query and answers the GRNs of the
// objects answered, in order.
func (s *listStack) listed(t *testing.T, path string, query url.Values, cookie *http.Cookie) []string {
	t.Helper()
	status, body := s.get(t, path, query, cookie)
	require.Equal(t, http.StatusOK, status, body)
	var objects []struct {
		Grn string `json:"grn"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &objects))
	out := make([]string, 0, len(objects))
	for _, o := range objects {
		out = append(out, o.Grn)
	}
	return out
}

// listKind is one org-gated list of addressable objects: its path, the
// read each of its objects is answered on, and how a test seeds one
// object of it. An object's own read is at the list's path and the
// object's identifier, followed by readSuffix.
type listKind struct {
	name       string
	path       string
	query      url.Values
	action     string
	readSuffix string
	seed       func(t *testing.T, s *listStack, name string) grn.GRN
}

func listKinds() []listKind {
	return []listKind{
		{name: "groups", path: "/orgs/acme/groups", action: org.ActionGroupRead, seed: seedListGroup},
		{name: "the caller's own groups", path: "/orgs/acme/groups", query: url.Values{"member": {"me"}},
			action: org.ActionGroupRead, seed: seedListGroup},
		{name: "projects", path: "/orgs/acme/projects", action: org.ActionProjectRead, seed: seedListProject},
		{name: "agents", path: "/orgs/acme/agents", action: org.ActionAgentRead,
			seed: func(t *testing.T, s *listStack, _ string) grn.GRN { return seedAgent(t, s.agents, s.acme) }},
		// A policy is read by its versions, the one read policy:read
		// gates on the policy itself.
		{name: "policies", path: "/orgs/acme/policies", action: org.ActionPolicyRead, readSuffix: "/versions",
			seed: func(t *testing.T, s *listStack, name string) grn.GRN {
				g, err := grn.Parse(activePolicy(t, s.policies, s.acme, name,
					statementDoc(name, wire.EffectAllow, org.ActionOrgRead, s.acme.String())))
				require.NoError(t, err)
				return g
			}},
	}
}

// seedListGroup creates a group both owners belong to, so member=me
// lists it for either.
func seedListGroup(t *testing.T, s *listStack, name string) grn.GRN {
	t.Helper()
	g := grn.New("acme", grn.DefaultProject, org.ServiceTypeGroup, name)
	require.NoError(t, s.orgs.CreateGroup(context.Background(), org.NewGroup(g, name, s.acme)))
	for _, m := range []grn.GRN{s.adaGRN, s.danGRN} {
		require.NoError(t, s.orgs.AddGroupMember(context.Background(), g, m))
	}
	return g
}

// seedListProject creates a project ada belongs to, holding the admin
// role, so a project's own statements reach her through it.
func seedListProject(t *testing.T, s *listStack, name string) grn.GRN {
	t.Helper()
	g := grn.New("acme", grn.DefaultProject, org.ServiceTypeProject, name)
	require.NoError(t, s.orgs.CreateProject(context.Background(), org.NewProject(g, name, s.acme, string(org.RoleAdmin))))
	require.NoError(t, s.orgs.AddProjectMember(context.Background(), g, s.adaGRN))
	return g
}

// TestLists_ADenyOnOneObjectHidesThatObjectAlone seeds three objects of
// each list and denies ada the read of the middle one alone. Her list
// before the Deny holds all three, which is the setup read back and the
// control that the list is what narrows. After it, her list is dan's
// without that object, and dan's still holds it: the Deny hid one
// object from one caller. Reading the hidden object itself is refused,
// so the list and the read agree.
func TestLists_ADenyOnOneObjectHidesThatObjectAlone(t *testing.T) {
	for _, k := range listKinds() {
		t.Run(k.name, func(t *testing.T) {
			s := newListStack(t)
			first, hidden, last := k.seed(t, s, "a-first"), k.seed(t, s, "b-hidden"), k.seed(t, s, "c-last")
			before := s.listed(t, k.path, k.query, s.ada)
			require.Subset(t, before, []string{first.String(), hidden.String(), last.String()}, "control, before the Deny")

			deny := activePolicy(t, s.policies, s.acme, "deny-one",
				statementDoc("deny-one", wire.EffectDeny, k.action, hidden.String()))
			attachPolicy(t, s.policies, deny, s.adaGRN)

			control := s.listed(t, k.path, k.query, s.dan)
			require.Contains(t, control, hidden.String(), "the Deny does not reach dan")
			want := slices.DeleteFunc(slices.Clone(control), func(g string) bool { return g == hidden.String() })
			assert.Equal(t, want, s.listed(t, k.path, k.query, s.ada), "ada's list is dan's without the denied object")

			status, body := s.get(t, k.path+"/"+hidden.Identifier+k.readSuffix, nil, s.ada)
			assert.Equal(t, http.StatusForbidden, status, "the read of the hidden object refuses as the list hides it: %s", body)
			status, body = s.get(t, k.path+"/"+first.Identifier+k.readSuffix, nil, s.ada)
			assert.Equal(t, http.StatusOK, status, "control: a sibling reads: %s", body)
		})
	}
}

// TestLists_AFilteredPageCanBeShortWithMoreAfterIt pages ada's groups
// two at a time with the middle of three denied: the first page holds
// one group and the second the last, so a short page is not the end
// and no group is skipped or repeated across the two.
func TestLists_AFilteredPageCanBeShortWithMoreAfterIt(t *testing.T) {
	s := newListStack(t)
	first, hidden, last := seedListGroup(t, s, "a-first"), seedListGroup(t, s, "b-hidden"), seedListGroup(t, s, "c-last")
	page := url.Values{"limit": {"2"}}
	require.Equal(t, []string{first.String(), hidden.String()}, s.listed(t, "/orgs/acme/groups", page, s.ada),
		"control, before the Deny: a full page")

	deny := activePolicy(t, s.policies, s.acme, "deny-one",
		statementDoc("deny-one", wire.EffectDeny, org.ActionGroupRead, hidden.String()))
	attachPolicy(t, s.policies, deny, s.adaGRN)

	assert.Equal(t, []string{first.String()}, s.listed(t, "/orgs/acme/groups", page, s.ada), "a short first page")
	page.Set("offset", "2")
	assert.Equal(t, []string{last.String()}, s.listed(t, "/orgs/acme/groups", page, s.ada), "with more after it")
}
