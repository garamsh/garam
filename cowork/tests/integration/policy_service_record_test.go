package integration_test

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

// refusedPlatform is the platform cowork keeps for who a caller is; it
// fails the test if asked what is allowed.
type refusedPlatform struct {
	platform.Platform
	t *testing.T
}

func (p refusedPlatform) Allowed(context.Context, platform.Caller, string, []platform.Check) ([]bool, error) {
	p.t.Error("the platform was asked what is allowed")
	return nil, nil
}

// TestPolicyServiceRecord_DecisionAndRefusalAreLogged records, for
// #1774's report, what cowork receives from the policy service for a
// cowork resource and what the service answers a write of a type
// cowork does not own. The policy service is NOT run here: it is the
// in-process fake of cowork/internal/policy/policytest, which applies
// the rule of policy/README.md §Callers. What it records is cowork's
// side of the contract against that rule as written, not the
// service's behaviour.
//
//	cd cowork && go test -count=1 -v -run TestPolicyServiceRecord \
//	    ./tests/integration/
func TestPolicyServiceRecord_DecisionAndRefusalAreLogged(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	server := policytest.Serve(t, endpoint)
	const pageID = "grn:acme/cowork/page/abcdefghijkl"
	server.Decide = func(org string, e *policyv1.Evaluation) bool {
		return org == "acme" && e.GetAction().GetName() == "cowork:page.read"
	}
	cfg := policy.Config{Addr: server.Addr, CAFile: server.CAFile, TokenURL: endpoint.URL, KeyFile: keyFile}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	ada := platform.Caller{GRN: grn.New(grn.RootOwner, "user", "ada")}

	got, err := platformpolicy.New(refusedPlatform{t: t}, client).Allowed(context.Background(), ada, "acme",
		[]platform.Check{platform.OnPage("cowork:page.read", "abcdefghijkl"), platform.OnPage("cowork:page.write", "abcdefghijkl")})
	require.NoError(t, err)
	asked := server.Asked()
	require.Len(t, asked, 1)
	t.Logf("decision received: caller %s, org acme, resource %s: cowork:page.read=%t cowork:page.write=%t "+
		"(request: %d evaluations, subject %s %s)", ada.GRN, pageID, got[0], got[1],
		len(asked[0].Request.GetEvaluations()), asked[0].Request.GetSubject().GetType(), asked[0].Request.GetSubject().GetId())
	assert.Equal(t, []bool{true, false}, got)

	_, err = client.ApplyEntityChanges(context.Background(), "acme", &policyv1.ApplyEntityChangesRequest{
		Changes: []*policyv1.EntityChange{{
			Entity:    &policyv1.EntityRef{Type: "Iam::Org", Id: grn.Org("acme").String()},
			Sequence:  1,
			Operation: &policyv1.EntityChange_Delete{Delete: &policyv1.EntityDelete{}},
		}},
	})
	require.Error(t, err)
	t.Logf("write of Iam::Org by cowork refused: kind %v: %v", errorx.KindOf(err), err)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
}
