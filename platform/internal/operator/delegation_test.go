package operator_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

var (
	acme       = grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
	hosted     = grn.New("acme", grn.DefaultProject, operator.ServiceType, "hosted")
	controller = grn.New("acme", grn.DefaultProject, operator.ServiceType, "controller")
)

// TestParseGrantTerms_BoundsWhatAGrantCanReach holds every bound a
// grant write is refused for at the boundary. The accepted terms are
// the control: each refusal differs from them in one field.
func TestParseGrantTerms_BoundsWhatAGrantCanReach(t *testing.T) {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	valid := func() ([]string, []string, time.Time) {
		return []string{controller.String()}, []string{"agent:configure", "agent:create"}, now.Add(30 * 24 * time.Hour)
	}
	controllers, operations, expiresAt := valid()
	terms, err := operator.ParseGrantTerms(acme, hosted, controllers, operations, expiresAt, now)
	require.NoError(t, err)
	assert.Equal(t, []operator.Operation{operator.OperationAgentConfigure, operator.OperationAgentCreate}, terms.Operations)

	many := make([]string, 33)
	for i := range many {
		many[i] = grn.New("acme", grn.DefaultProject, operator.ServiceType, "c"+strings.Repeat("x", i+1)).String()
	}
	cases := map[string]func() ([]string, []string, time.Time){
		"no controller":  func() ([]string, []string, time.Time) { _, o, e := valid(); return nil, o, e },
		"33 controllers": func() ([]string, []string, time.Time) { _, o, e := valid(); return many, o, e },
		"another organization": func() ([]string, []string, time.Time) {
			_, o, e := valid()
			return []string{"grn:globex:default:operator:c"}, o, e
		},
		"not an operator": func() ([]string, []string, time.Time) {
			_, o, e := valid()
			return []string{"grn:acme:default:agent:c"}, o, e
		},
		"the hosted operator itself": func() ([]string, []string, time.Time) { _, o, e := valid(); return []string{hosted.String()}, o, e },
		"a controller twice": func() ([]string, []string, time.Time) {
			_, o, e := valid()
			return []string{controller.String(), controller.String()}, o, e
		},
		"no operation":         func() ([]string, []string, time.Time) { c, _, e := valid(); return c, nil, e },
		"an unknown operation": func() ([]string, []string, time.Time) { c, _, e := valid(); return c, []string{"agent:*"}, e },
		"already expired":      func() ([]string, []string, time.Time) { c, o, _ := valid(); return c, o, now },
		"past thirty days": func() ([]string, []string, time.Time) {
			c, o, _ := valid()
			return c, o, now.Add(30*24*time.Hour + time.Second)
		},
	}
	for name, build := range cases {
		c, o, e := build()
		_, err := operator.ParseGrantTerms(acme, hosted, c, o, e, now)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%s: %v", name, err)
	}
}

// TestGrantCovers_RequiresEveryBound holds that a grant covers an
// operation only while current, unexpired, carrying the operation, and
// naming the controller.
func TestGrantCovers_RequiresEveryBound(t *testing.T) {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	g := &operator.Grant{Controllers: []grn.GRN{controller}, ExpiresAt: now.Add(time.Hour),
		Operations: []operator.Operation{operator.OperationAgentCreate}}
	assert.True(t, g.Covers(operator.OperationAgentCreate, controller, now))
	assert.False(t, g.Covers(operator.OperationAgentConfigure, controller, now))
	assert.False(t, g.Covers(operator.OperationAgentCreate, hosted, now))
	assert.False(t, g.Covers(operator.OperationAgentCreate, controller, now.Add(time.Hour)))
	ended := now
	g.EndedAt = &ended
	assert.False(t, g.Covers(operator.OperationAgentCreate, controller, now))
}

// TestParseBodyDigestAndRequestID_AcceptOnlyTheirExactForms holds the
// two wire forms a request identity is built from.
func TestParseBodyDigestAndRequestID_AcceptOnlyTheirExactForms(t *testing.T) {
	_, err := operator.ParseBodyDigest(strings.Repeat("0f", 32))
	require.NoError(t, err)
	for _, bad := range []string{strings.Repeat("0F", 32), strings.Repeat("0", 63), strings.Repeat("g", 64)} {
		_, err := operator.ParseBodyDigest(bad)
		assert.Error(t, err, bad)
	}
	_, err = operator.ParseRequestID("create.agent-1~retry_2")
	require.NoError(t, err)
	for _, bad := range []string{"", strings.Repeat("a", 129), "has space", "slash/es"} {
		_, err := operator.ParseRequestID(bad)
		assert.Error(t, err, bad)
	}
}

// TestOperationIsMintable_ExcludesActivation holds that a member can
// hand over creation and configuration and never activation.
func TestOperationIsMintable_ExcludesActivation(t *testing.T) {
	assert.True(t, operator.OperationAgentCreate.IsMintable())
	assert.True(t, operator.OperationAgentConfigure.IsMintable())
	assert.False(t, operator.OperationAgentActivate.IsMintable())
}

// TestGrantCovers_ReadsNoControllerForAnOrganizationOperation holds
// that an operation on what the Operator holds for the organization
// is covered whatever controllers the grant lists. An operation on an
// agent, under the same grant and the same controller argument, is the
// control: it still needs the controller named.
func TestGrantCovers_ReadsNoControllerForAnOrganizationOperation(t *testing.T) {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	g := &operator.Grant{Controllers: []grn.GRN{controller}, ExpiresAt: now.Add(time.Hour),
		Operations: []operator.Operation{operator.OperationTemplateRead, operator.OperationExecutionRead}}
	assert.True(t, g.Covers(operator.OperationTemplateRead, grn.GRN{}, now))
	assert.False(t, g.Covers(operator.OperationExecutionRead, grn.GRN{}, now))
	assert.True(t, g.Covers(operator.OperationExecutionRead, controller, now))
	assert.False(t, g.Covers(operator.OperationTemplatePublish, grn.GRN{}, now), "an operation the grant does not list")
}

// TestOperation_BindsRequestTargetExactlyForTheReadsAndPublication
// holds which operations bind a request target: the four #1170 added
// and operator:read (#1374), and none of the operations that bind only
// their body.
func TestOperation_BindsRequestTargetExactlyForTheReadsAndPublication(t *testing.T) {
	for _, op := range []operator.Operation{operator.OperationTemplateRead, operator.OperationTemplatePublish,
		operator.OperationProfileRead, operator.OperationExecutionRead, operator.OperationOperatorRead} {
		assert.True(t, op.BindsRequestTarget(), op)
		assert.True(t, op.IsMintable(), op)
		parsed, err := operator.ParseOperation(string(op))
		require.NoError(t, err)
		assert.Equal(t, op, parsed)
	}
	for _, op := range []operator.Operation{operator.OperationAgentCreate, operator.OperationAgentConfigure,
		operator.OperationAgentRecover, operator.OperationAgentActivate} {
		assert.False(t, op.BindsRequestTarget(), op)
	}
}

// TestParseRequestTarget_AcceptsOnlyOriginForm holds the bound on what
// an authority binds as its request target. Each refusal differs from
// an accepted target in the one property it breaks.
func TestParseRequestTarget_AcceptsOnlyOriginForm(t *testing.T) {
	for _, ok := range []string{
		"/v1/orgs/acme/templates",
		"/prefix/v1/orgs/acme/templates/web%20agent/versions/3",
		"/v1/orgs/acme/agents/grn:acme:default:agent:9f2a/execution",
		"/v1/orgs/acme/templates?after=b&after=a",
		"/v1/orgs/acme/templates?next=../a",
		"/v1/orgs/acme/templates/v1.2",
		"/" + strings.Repeat("a", 2047),
	} {
		got, err := operator.ParseRequestTarget(ok)
		require.NoError(t, err, ok)
		assert.Equal(t, operator.RequestTarget(ok), got)
	}
	for _, bad := range []string{
		"",
		"v1/orgs/acme/templates",
		"https://control.example.com/v1/orgs/acme/templates",
		"//control.example.com/v1/orgs/acme/templates",
		"/v1/orgs//templates",
		"/v1/orgs/acme/templates/",
		"/v1/orgs/acme/../globex/templates",
		"/v1/orgs/acme/./templates",
		"/v1/orgs/acme/%2E%2e/globex/templates",
		"/v1/orgs/acme/templates/..",
		"/v1/orgs/acme/templates#fragment",
		"/v1/orgs/acme/templ ates",
		"/v1/orgs/acme/templates%2",
		"/v1/orgs/acme/templates%zz",
		"/v1/orgs/acme/\ttemplates",
		"/" + strings.Repeat("a", 2048),
	} {
		_, err := operator.ParseRequestTarget(bad)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%q: %v", bad, err)
	}
}
