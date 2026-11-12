package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/platform"
	platformpolicy "github.com/garamsh/garam/cowork/internal/platform/policy"
	"github.com/garamsh/garam/cowork/internal/policy"
	"github.com/garamsh/garam/cowork/internal/policy/policytest"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
)

// base is the platform as cowork has it where the policy service is
// not asked: it answers who a credential is and which organizations it
// has, and fails the test if asked what is allowed.
type base struct {
	t *testing.T
}

func (b base) Identify(_ context.Context, cred platform.Credential) (platform.Caller, error) {
	return platform.Caller{GRN: grn.New(grn.RootOwner, "user", "ada"), Credential: cred}, nil
}

func (b base) Allowed(context.Context, platform.Caller, string, []platform.Check) ([]bool, error) {
	b.t.Error("the platform was asked what is allowed")
	return nil, nil
}

func (base) Organizations(context.Context, platform.Caller) ([]platform.Organization, error) {
	return []platform.Organization{{ID: "acme", Name: "Acme"}}, nil
}

type fixture struct {
	server   *policytest.Server
	endpoint *policytest.Endpoint
	adapter  *platformpolicy.Platform
	ada      platform.Caller
}

// newFixture stands up the fakes and an adapter whose service allows
// what allowed names, keyed by action@resource, to ada in acme.
func newFixture(t *testing.T, allowed map[string]bool) *fixture {
	t.Helper()
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	server := policytest.Serve(t, endpoint)
	server.Decide = func(org string, e *policyv1.Evaluation) bool {
		return org == "acme" && allowed[e.GetAction().GetName()+"@"+e.GetResource().GetId()]
	}
	cfg := policy.Config{Addr: server.Addr, CAFile: server.CAFile, TokenURL: endpoint.URL, KeyFile: keyFile}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return &fixture{
		server: server, endpoint: endpoint,
		adapter: platformpolicy.New(base{t}, client),
		ada:     platform.Caller{GRN: grn.New(grn.RootOwner, "user", "ada")},
	}
}

const (
	orgID  = "grn:root/iam/org/acme"
	pageID = "grn:acme/cowork/page/abcdefghijkl"
)

func TestAllowed_AsksOneEvaluationsForTheBatchAndMapsEachAnswer(t *testing.T) {
	f := newFixture(t, map[string]bool{
		"cowork:page.read@" + orgID:                                true,
		"cowork:page.read@" + pageID:                               true,
		"cowork:channel.post@grn:acme/cowork/channel/abcdefghijkl": false,
		"cowork:task.write@grn:acme/cowork/task/abcdefghijkl":      true,
	})
	checks := []platform.Check{
		{Action: "cowork:page.read"},
		platform.OnPage("cowork:page.read", "abcdefghijkl"),
		platform.OnChannel("cowork:channel.post", "abcdefghijkl"),
		platform.OnTask("cowork:task.write", "abcdefghijkl"),
	}

	got, err := f.adapter.Allowed(context.Background(), f.ada, "acme", checks)

	require.NoError(t, err)
	assert.Equal(t, []bool{true, true, false, true}, got)
	asked := f.server.Asked()
	require.Len(t, asked, 1, "one call for the whole batch")
	req := asked[0].Request
	assert.Equal(t, "Iam::User", req.GetSubject().GetType())
	assert.Equal(t, "grn:root/iam/user/ada", req.GetSubject().GetId())
	var got2 []string
	for _, e := range req.GetEvaluations() {
		got2 = append(got2, e.GetAction().GetName()+" "+e.GetResource().GetType()+" "+e.GetResource().GetId())
	}
	assert.Equal(t, []string{
		"cowork:page.read Iam::Org " + orgID,
		"cowork:page.read Cowork::Page " + pageID,
		"cowork:channel.post Cowork::Channel grn:acme/cowork/channel/abcdefghijkl",
		"cowork:task.write Cowork::Task grn:acme/cowork/task/abcdefghijkl",
	}, got2, "no question beyond the batch's own")
}

func TestAllowed_AnAgentIsAskedAsAnAgent(t *testing.T) {
	f := newFixture(t, map[string]bool{"cowork:page.read@" + orgID: true})
	agent := platform.Caller{GRN: grn.New("acme", "agent", "bot")}

	_, err := f.adapter.Allowed(context.Background(), agent, "acme", nil)

	require.NoError(t, err)
	subject := f.server.Asked()[0].Request.GetSubject()
	assert.Equal(t, "Iam::Agent", subject.GetType())
	assert.Equal(t, "grn:acme/iam/agent/bot", subject.GetId())
}

func TestAllowed_ANonMemberIsDeniedAsThePlatformDeniesOne(t *testing.T) {
	f := newFixture(t, map[string]bool{"cowork:page.read@" + pageID: true})
	f.server.Member = func(_, subject string) bool { return subject != "grn:root/iam/user/eve" }
	eve := platform.Caller{GRN: grn.New(grn.RootOwner, "user", "eve")}

	got, err := f.adapter.Allowed(context.Background(), eve, "acme",
		[]platform.Check{platform.OnPage("cowork:page.read", "abcdefghijkl")})
	assert.Nil(t, got)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))

	// Control: a member of the same organization is answered.
	got, err = f.adapter.Allowed(context.Background(), f.ada, "acme",
		[]platform.Check{platform.OnPage("cowork:page.read", "abcdefghijkl")})
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, got)
}

func TestAllowed_AnEmptyBatchAsksOneQuestionAndJudgesItByItsReasons(t *testing.T) {
	f := newFixture(t, nil)
	f.server.Member = func(_, subject string) bool { return subject != "grn:root/iam/user/eve" }
	eve := platform.Caller{GRN: grn.New(grn.RootOwner, "user", "eve")}

	_, err := f.adapter.Allowed(context.Background(), eve, "acme", nil)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))

	// A member denied the question is still a member.
	got, err := f.adapter.Allowed(context.Background(), f.ada, "acme", nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	require.Len(t, f.server.Asked()[1].Request.GetEvaluations(), 1)
}

func TestAllowed_AMemberAGuardrailForbidsPageReadStillGetsEachChecksDecision(t *testing.T) {
	channelID := "grn:acme/cowork/channel/abcdefghijkl"
	f := newFixture(t, map[string]bool{"cowork:channel.read@" + channelID: true})

	got, err := f.adapter.Allowed(context.Background(), f.ada, "acme", []platform.Check{
		platform.OnPage("cowork:page.read", "abcdefghijkl"),
		platform.OnChannel("cowork:channel.read", "abcdefghijkl"),
	})

	require.NoError(t, err)
	assert.Equal(t, []bool{false, true}, got)
}

func TestAllowed_AnUnreachableServiceIsUnavailable(t *testing.T) {
	f := newFixture(t, nil)
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	cfg := policy.Config{Addr: "127.0.0.1:1", CAFile: f.server.CAFile, TokenURL: endpoint.URL, KeyFile: keyFile}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	adapter := platformpolicy.New(base{t}, client)

	_, err = adapter.Allowed(context.Background(), f.ada, "acme", []platform.Check{{Action: "cowork:page.read"}})

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}

func TestAllowed_ATokenEndpointThatRefusesIsUnavailable(t *testing.T) {
	f := newFixture(t, nil)
	// A deployment key the endpoint does not trust.
	otherKey, _ := policytest.Key(t)
	cfg := policy.Config{Addr: f.server.Addr, CAFile: f.server.CAFile, TokenURL: f.endpoint.URL, KeyFile: otherKey}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = platformpolicy.New(base{t}, client).Allowed(context.Background(), f.ada, "acme",
		[]platform.Check{{Action: "cowork:page.read"}})

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	assert.Empty(t, f.server.Asked(), "no question is asked without a token")
}

func TestAllowed_AskedUnderTheTokenOfTheOrganizationAsked(t *testing.T) {
	f := newFixture(t, map[string]bool{"cowork:page.read@" + orgID: true})
	_, err := f.adapter.Allowed(context.Background(), f.ada, "acme", nil)
	require.NoError(t, err)

	asked := f.server.Asked()
	require.Len(t, asked, 1)
	org, ok := f.endpoint.Org(asked[0].Token)
	require.True(t, ok)
	assert.Equal(t, "acme", org)
}

func TestAllowed_AResourceNoGRNCanNameIsDeniedWithoutBeingAsked(t *testing.T) {
	f := newFixture(t, map[string]bool{"cowork:page.read@" + orgID: true, "cowork:page.read@" + pageID: true})

	got, err := f.adapter.Allowed(context.Background(), f.ada, "acme", []platform.Check{
		platform.OnPage("cowork:page.read", "abcdefghijkl"),
		platform.OnPage("cowork:page.read", "../other/org"),
		platform.OnPage("cowork:page.read", "ABC"),
	})

	require.NoError(t, err)
	assert.Equal(t, []bool{true, false, false}, got)
	assert.Len(t, f.server.Asked()[0].Request.GetEvaluations(), 1, "the page alone")
}

func TestAllowed_IdentifyAndOrganizationsStayWithThePlatform(t *testing.T) {
	f := newFixture(t, nil)

	caller, err := f.adapter.Identify(context.Background(), platform.Credential{Bearer: "b"})
	require.NoError(t, err)
	assert.Equal(t, "ada", caller.GRN.Identifier)
	orgs, err := f.adapter.Organizations(context.Background(), caller)
	require.NoError(t, err)
	assert.Equal(t, []platform.Organization{{ID: "acme", Name: "Acme"}}, orgs)
	assert.Empty(t, f.server.Asked())
}
