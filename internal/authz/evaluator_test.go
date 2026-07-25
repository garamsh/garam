package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/grn"
)

type testObject struct{ g grn.GRN }

func (o testObject) GRN() grn.GRN { return o.g }
func (o testObject) Type() string { return o.g.ServiceType }

func evaluate(statements []authz.Statement, action, resource string) authz.Decision {
	ev := authz.NewStatementEvaluator(statements)
	g, err := grn.Parse(resource)
	if err != nil {
		panic(err)
	}
	return ev.Evaluate(context.Background(), authz.Context{Object: testObject{g}, Action: action})
}

func TestStatementEvaluator_ExplicitDenyWins(t *testing.T) {
	statements := []authz.Statement{
		{Sid: "allow", Effect: authz.EffectAllow, Action: []string{"org:*"}, Resource: []string{"grn:root:default:org:acme"}},
		{Sid: "deny", Effect: authz.EffectDeny, Action: []string{"org:delete"}, Resource: []string{"grn:root:default:org:acme"}},
	}
	got := evaluate(statements, "org:delete", "grn:root:default:org:acme")
	assert.Equal(t, authz.DecisionDeny, got)

	// Deny only covers org:delete; other actions still allowed.
	got = evaluate(statements, "org:read", "grn:root:default:org:acme")
	assert.Equal(t, authz.DecisionAllow, got)
}

func TestStatementEvaluator_DefaultDeny(t *testing.T) {
	statements := []authz.Statement{
		{Sid: "allow", Effect: authz.EffectAllow, Action: []string{"org:read"}, Resource: []string{"grn:root:default:org:acme"}},
	}
	cases := []struct {
		name             string
		action, resource string
		want             authz.Decision
	}{
		{"matching allow", "org:read", "grn:root:default:org:acme", authz.DecisionAllow},
		{"unlisted action", "org:update", "grn:root:default:org:acme", authz.DecisionDeny},
		{"other resource", "org:read", "grn:root:default:org:other", authz.DecisionDeny},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, evaluate(statements, c.action, c.resource))
		})
	}
}

func TestStatementEvaluator_Patterns(t *testing.T) {
	statements := []authz.Statement{
		{Sid: "wild", Effect: authz.EffectAllow, Action: []string{"org:*"}, Resource: []string{"grn:root:default:org:*"}},
	}
	assert.Equal(t, authz.DecisionAllow, evaluate(statements, "org:read", "grn:root:default:org:acme"))
	assert.Equal(t, authz.DecisionAllow, evaluate(statements, "org:delete", "grn:root:default:org:beta"))
	assert.Equal(t, authz.DecisionDeny, evaluate(statements, "user:read", "grn:root:default:org:acme"))
	assert.Equal(t, authz.DecisionDeny, evaluate(statements, "org:read", "grn:acme:default:user:alice"))
}

func TestStatement_Matches(t *testing.T) {
	s := authz.Statement{Effect: authz.EffectAllow, Action: []string{"*"}, Resource: []string{"*"}}
	assert.True(t, s.Matches("anything", "grn:root:default:org:acme"))

	s = authz.Statement{Effect: authz.EffectAllow, Action: []string{"org:read"}, Resource: []string{"grn:root:default:org:acme"}}
	assert.False(t, s.Matches("org:read", "grn:root:default:org:other"))
}

func TestStatement_MatchesFullPatterns(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{"star crosses segments", "grn:acme:*", "grn:acme:default:user:alice", true},
		{"star matches empty run", "grn:acme:*", "grn:acme:", true},
		{"mid-pattern star", "grn:*:user:alice", "grn:acme:default:user:alice", true},
		{"mid-pattern star mismatch", "grn:*:user:alice", "grn:acme:default:agent:alice", false},
		{"multiple stars", "grn:*:default:*:alice", "grn:acme:default:user:alice", true},
		{"question mark one char", "org:updat?", "org:update", true},
		{"question mark needs a char", "org:updat?", "org:updat", false},
		{"question mark not two chars", "grn:acme:default:org:a?me", "grn:acme:default:org:acme", true},
		{"star then literal suffix", "grn:*alice", "grn:acme:default:user:alice", true},
		{"star then literal suffix mismatch", "grn:*bob", "grn:acme:default:user:alice", false},
		{"exact", "org:read", "org:read", true},
		{"exact mismatch", "org:read", "org:write", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := authz.Statement{Effect: authz.EffectAllow, Action: []string{c.pattern}, Resource: []string{"*"}}
			assert.Equal(t, c.want, s.Matches(c.value, "grn:root:default:org:acme"))
		})
	}
}
