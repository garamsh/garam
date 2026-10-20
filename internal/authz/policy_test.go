package authz_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
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
	assert.Equal(t, wire.EffectAllow, doc.Statement[0].Effect)
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

// TestParsePolicyDocument_ReMarshalsToWhatWasSubmitted pins the parse
// boundary as lossless: what it returns encodes back to the document it
// was handed. Condition is the reserved field #724 found dropped there;
// the document without one is beside it so that a Condition appearing
// where none was written fails too.
func TestParsePolicyDocument_ReMarshalsToWhatWasSubmitted(t *testing.T) {
	docs := map[string]string{
		"carrying a condition": `{"Version":"2026-07-25","Statement":[{"Sid":"OfficeOnly","Effect":"Allow","Action":["org:update"],"Resource":["grn:acme:*"],"Condition":{"IpAddress":{"garam:SourceIp":"203.0.113.0/24"}}}]}`,
		"carrying none":        `{"Version":"2026-07-25","Statement":[{"Sid":"AllowOrgRead","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`,
	}
	for name, submitted := range docs {
		t.Run(name, func(t *testing.T) {
			doc, err := authz.ParsePolicyDocument([]byte(submitted))
			require.NoError(t, err)
			raw, err := json.Marshal(doc)
			require.NoError(t, err)
			assert.JSONEq(t, submitted, string(raw))
		})
	}
}

// TestParsePolicyDocument_RefusesWhatItCannotCarry covers the input the
// boundary can neither carry nor drop in silence. Each case names the
// same document without what is under test, and that one is accepted: a
// refusal the control draws as well came from something else.
func TestParsePolicyDocument_RefusesWhatItCannotCarry(t *testing.T) {
	const accepted = `{"Version":"v","Statement":[{"Effect":"Allow","Action":["a"],"Resource":["r"]}]}`
	cases := map[string]string{
		"a key the statement shape does not name": `{"Version":"v","Statement":[{"Effect":"Allow","Action":["a"],"Resource":["r"],"Conditon":{}}]}`,
		"a key the document shape does not name":  `{"Version":"v","Id":"p-1","Statement":[{"Effect":"Allow","Action":["a"],"Resource":["r"]}]}`,
		"a second document after the first":       accepted + `{"Version":"w"}`,
	}
	for name, refused := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := authz.ParsePolicyDocument([]byte(refused))
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))

			_, err = authz.ParsePolicyDocument([]byte(accepted))
			assert.NoError(t, err, "the same document without it parses, so the refusal above is this boundary's")
		})
	}
}

func TestParsePolicyStatus(t *testing.T) {
	for _, s := range []wire.PolicyStatus{wire.PolicyStatusDraft, wire.PolicyStatusActive} {
		got, err := authz.ParsePolicyStatus(string(s))
		require.NoError(t, err)
		assert.Equal(t, s, got)
	}
	// archived was a status until ADR-0117 replaced it with deletion.
	_, err := authz.ParsePolicyStatus("archived")
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
	assert.Equal(t, wire.PolicyStatusDraft, p.Status)
	assert.Equal(t, 1, p.DefaultVersion)
	assert.Equal(t, acme, p.Organization)
	assert.Equal(t, "read-only", p.Name)
}
