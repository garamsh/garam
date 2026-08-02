package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

func TestParsePolicyDocument_Valid(t *testing.T) {
	doc, err := authz.ParsePolicyDocument([]byte(`{
	  "Version": "2026-07-25",
	  "Statement": [
	    {"Sid": "AllowOrgRead", "Effect": "Allow", "Action": ["org:read"], "Resource": ["grn:acme:*"]},
	    {"Sid": "DenyDelete", "Effect": "Deny", "Action": ["org:delete"], "Resource": ["grn:acme:*"]}
	  ]
	}`))
	require.NoError(t, err)
	assert.Equal(t, "2026-07-25", doc.Version)
	require.Len(t, doc.Statement, 2)
	assert.Equal(t, authz.EffectAllow, doc.Statement[0].Effect)
	assert.Equal(t, []string{"org:delete"}, doc.Statement[1].Action)
}

func TestParsePolicyDocument_Rejects(t *testing.T) {
	cases := map[string]string{
		"malformed json":  `{`,
		"missing version": `{"Statement": [{"Effect": "Allow", "Action": ["a"], "Resource": ["r"]}]}`,
		"no statements":   `{"Version": "v", "Statement": []}`,
		"unknown effect":  `{"Version": "v", "Statement": [{"Effect": "Maybe", "Action": ["a"], "Resource": ["r"]}]}`,
		"no action":       `{"Version": "v", "Statement": [{"Effect": "Allow", "Resource": ["r"]}]}`,
		"no resource":     `{"Version": "v", "Statement": [{"Effect": "Allow", "Action": ["a"]}]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := authz.ParsePolicyDocument([]byte(raw))
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

func TestParsePolicyStatus(t *testing.T) {
	for _, s := range []authz.PolicyStatus{authz.PolicyStatusDraft, authz.PolicyStatusActive, authz.PolicyStatusArchived} {
		got, err := authz.ParsePolicyStatus(string(s))
		require.NoError(t, err)
		assert.Equal(t, s, got)
	}
	_, err := authz.ParsePolicyStatus("pending")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

func TestNewPolicy_StartsAsADraftOnTheFirstVersion(t *testing.T) {
	acme := orgGRN("acme")
	g, err := authz.NewPolicyGRN(acme)
	require.NoError(t, err)
	assert.Equal(t, "acme", g.Organization)
	assert.Equal(t, grn.DefaultProject, g.Project)
	assert.Equal(t, authz.ServiceTypePolicy, g.ServiceType)

	p := authz.NewPolicy(g, acme, "read-only")
	assert.Equal(t, authz.PolicyStatusDraft, p.Status())
	assert.Equal(t, 1, p.DefaultVersion())
	assert.Equal(t, acme, p.Organization())
	assert.Equal(t, "read-only", p.Name())
}
