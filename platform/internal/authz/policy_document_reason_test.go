package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// TestParsePolicyDocument_NamesTheRuleAndThePlace drives one document
// per rule the parser holds and reads the reason and pointer each
// refusal carries. The valid document is the control: the same parser
// accepts it, so each refusal below is its rule's and not a parser
// that refuses everything.
func TestParsePolicyDocument_NamesTheRuleAndThePlace(t *testing.T) {
	const valid = `{"Version":"2026-07-25","Statement":[{"Sid":"a","Effect":"Allow","Action":["org:read"],"Resource":["*"]}]}`
	_, err := authz.ParsePolicyDocument([]byte(valid))
	require.NoError(t, err, "the control: a document every rule accepts")

	tests := []struct {
		name     string
		document string
		code     string
		path     string
	}{
		{name: "not JSON", document: `{`, code: authz.ReasonPolicyDocumentMalformed, path: ""},
		{name: "not an object", document: `[]`, code: authz.ReasonPolicyDocumentWrongType, path: ""},
		{name: "trailing data", document: valid + `{}`, code: authz.ReasonPolicyDocumentTrailingData, path: ""},
		{name: "unknown key at the root", document: `{"Version":"v","Bad":1}`, code: authz.ReasonPolicyDocumentUnknownKey, path: "/Bad"},
		{name: "unknown key in a statement", document: `{"Version":"v","Statement":[{},{"Sid":"b","Bogus/x":1}]}`, code: authz.ReasonPolicyDocumentUnknownKey, path: "/Statement/1/Bogus~1x"},
		{name: "wrong type", document: `{"Version":1}`, code: authz.ReasonPolicyDocumentWrongType, path: "/Version"},
		{name: "wrong type, pointed at by the canonical name", document: `{"version":1}`, code: authz.ReasonPolicyDocumentWrongType, path: "/Version"},
		{name: "wrong type in a statement written in lower case", document: `{"Version":"v","statement":[{"effect":3}]}`, code: authz.ReasonPolicyDocumentWrongType, path: "/Statement/0/Effect"},
		{name: "wrong type in a list", document: `{"Version":"v","Statement":[{"Action":["a",3]}]}`, code: authz.ReasonPolicyDocumentWrongType, path: "/Statement/0/Action/1"},
		{name: "no version", document: `{"Statement":[]}`, code: authz.ReasonPolicyDocumentVersionMissing, path: "/Version"},
		{name: "no statement", document: `{"Version":"v","Statement":[]}`, code: authz.ReasonPolicyDocumentStatementEmpty, path: "/Statement"},
		{name: "effect", document: `{"Version":"v","Statement":[{"Effect":"Allow","Action":["a"],"Resource":["r"]},{"Effect":"Maybe"}]}`, code: authz.ReasonPolicyDocumentEffectInvalid, path: "/Statement/1/Effect"},
		{name: "action", document: `{"Version":"v","Statement":[{"Effect":"Deny","Resource":["r"]}]}`, code: authz.ReasonPolicyDocumentActionMissing, path: "/Statement/0/Action"},
		{name: "resource", document: `{"Version":"v","Statement":[{"Effect":"Deny","Action":["a"]}]}`, code: authz.ReasonPolicyDocumentResourceMissing, path: "/Statement/0/Resource"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authz.ParsePolicyDocument([]byte(tc.document))
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
			reason, ok := errorx.ReasonOf(err)
			require.True(t, ok, "the refusal carries a reason")
			assert.Equal(t, errorx.Reason{Code: tc.code, Path: tc.path}, reason)
		})
	}
}
