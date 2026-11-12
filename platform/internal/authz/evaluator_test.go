package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/wire"
)

type testObject struct{ g grn.GRN }

func (o testObject) GRN() grn.GRN { return o.g }

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
		{Sid: "allow", Effect: wire.EffectAllow, Action: []string{"iam:org.*"}, Resource: []string{"grn:root/iam/org/acme"}},
		{Sid: "deny", Effect: wire.EffectDeny, Action: []string{"iam:org.delete"}, Resource: []string{"grn:root/iam/org/acme"}},
	}
	got := evaluate(statements, "iam:org.delete", "grn:root/iam/org/acme")
	assert.Equal(t, authz.DecisionDeny, got)

	// Deny only covers iam:org.delete; other actions still allowed.
	got = evaluate(statements, "iam:org.read", "grn:root/iam/org/acme")
	assert.Equal(t, authz.DecisionAllow, got)
}

func TestStatementEvaluator_ScopedDenyOverridesOrgWideAllow(t *testing.T) {
	statements := []authz.Statement{
		{Sid: "org-role-owner", Effect: wire.EffectAllow, Action: []string{"iam:group.*"}, Resource: []string{"grn:acme/*"}},
		{Sid: "deny-eng", Effect: wire.EffectDeny, Action: []string{"iam:group.delete"}, Resource: []string{"grn:acme/iam/group/eng"}},
	}
	assert.Equal(t, authz.DecisionDeny, evaluate(statements, "iam:group.delete", "grn:acme/iam/group/eng"))

	// The deny is scoped: sibling groups and other actions stand.
	assert.Equal(t, authz.DecisionAllow, evaluate(statements, "iam:group.delete", "grn:acme/iam/group/ops"))
	assert.Equal(t, authz.DecisionAllow, evaluate(statements, "iam:group.update", "grn:acme/iam/group/eng"))
}

func TestStatementEvaluator_DefaultDeny(t *testing.T) {
	statements := []authz.Statement{
		{Sid: "allow", Effect: wire.EffectAllow, Action: []string{"iam:org.read"}, Resource: []string{"grn:root/iam/org/acme"}},
	}
	cases := []struct {
		name             string
		action, resource string
		want             authz.Decision
	}{
		{"matching allow", "iam:org.read", "grn:root/iam/org/acme", authz.DecisionAllow},
		{"unlisted action", "iam:org.update", "grn:root/iam/org/acme", authz.DecisionDeny},
		{"other resource", "iam:org.read", "grn:root/iam/org/other", authz.DecisionDeny},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, evaluate(statements, c.action, c.resource))
		})
	}
}

func TestStatementEvaluator_Patterns(t *testing.T) {
	statements := []authz.Statement{
		{Sid: "wild", Effect: wire.EffectAllow, Action: []string{"iam:org.*"}, Resource: []string{"grn:root/iam/org/*"}},
	}
	assert.Equal(t, authz.DecisionAllow, evaluate(statements, "iam:org.read", "grn:root/iam/org/acme"))
	assert.Equal(t, authz.DecisionAllow, evaluate(statements, "iam:org.delete", "grn:root/iam/org/beta"))
	assert.Equal(t, authz.DecisionDeny, evaluate(statements, "user:read", "grn:root/iam/org/acme"))
	assert.Equal(t, authz.DecisionDeny, evaluate(statements, "iam:org.read", "grn:acme/iam/user/alice"))
}

func TestStatement_Matches(t *testing.T) {
	s := authz.Statement{Effect: wire.EffectAllow, Action: []string{"*"}, Resource: []string{"*"}}
	assert.True(t, s.Matches("anything", "grn:root/iam/org/acme"))

	s = authz.Statement{Effect: wire.EffectAllow, Action: []string{"iam:org.read"}, Resource: []string{"grn:root/iam/org/acme"}}
	assert.False(t, s.Matches("iam:org.read", "grn:root/iam/org/other"))
}

func TestStatement_MatchesFullPatterns(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{"star crosses segments", "grn:acme/*", "grn:acme/iam/user/alice", true},
		{"star matches empty run", "grn:acme/*", "grn:acme/", true},
		{"mid-pattern star", "grn:*/user/alice", "grn:acme/iam/user/alice", true},
		{"mid-pattern star mismatch", "grn:*/user/alice", "grn:acme/iam/agent/alice", false},
		{"multiple stars", "grn:*/iam/*/alice", "grn:acme/iam/user/alice", true},
		{"question mark one char", "iam:org.updat?", "iam:org.update", true},
		{"question mark needs a char", "iam:org.updat?", "iam:org.updat", false},
		{"question mark not two chars", "grn:acme/iam/org/a?me", "grn:acme/iam/org/acme", true},
		{"star then literal suffix", "grn:*alice", "grn:acme/iam/user/alice", true},
		{"star then literal suffix mismatch", "grn:*bob", "grn:acme/iam/user/alice", false},
		{"exact", "iam:org.read", "iam:org.read", true},
		{"exact mismatch", "iam:org.read", "iam:org.write", false},
		// A pattern carrying no wildcard has nothing to backtrack to,
		// so a value it does not match is refused at the first
		// character that differs rather than realigned past it. The row
		// above differs late and this one early; a matcher that
		// realigns passes on the first and not on the second.
		{"exact mismatch in the first character", "iam:org.read", "aam:org.read", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := authz.Statement{Effect: wire.EffectAllow, Action: []string{c.pattern}, Resource: []string{"*"}}
			assert.Equal(t, c.want, s.Matches(c.value, "grn:root/iam/org/acme"))
		})
	}
}
