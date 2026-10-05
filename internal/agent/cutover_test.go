package agent_test

import (
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
)

// TestSourceDigest_IsTheSHA256OfTheRFC8785JSON pins the digest the
// Operator computes on its side to a canonical text written out by
// hand: members ordered by UTF-16 code units, and only the quote, the
// backslash and the control characters escaped. encoding/json's own
// text of the same object is the control: it escapes <, >, & and
// U+2028 and orders keys by bytes, so a digest over it differs.
func TestSourceDigest_IsTheSHA256OfTheRFC8785JSON(t *testing.T) {
	agentGRN := grn.New("acme", grn.DefaultProject, agent.ServiceType, "a1")
	operatorGRN := grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "o1")
	values := agent.Values{
		"b":          "2",
		"a":          "q\"<&> é\x01\\\n",
		"￿":          "last by UTF-16",
		"\U0001F600": "before U+FFFF by UTF-16, after it by bytes",
	}

	const canonical = `{"agentGrn":"grn:acme:default:agent:a1","operatorGrn":"grn:acme:default:operator:o1",` +
		`"values":{"a":"q\"<&>` + " " + `é\u0001\\\n","b":"2",` +
		`"` + "\U0001F600" + `":"before U+FFFF by UTF-16, after it by bytes","` + "￿" + `":"last by UTF-16"}}`
	want := sha256.Sum256([]byte(canonical))
	assert.Equal(t, agent.Digest(want), agent.SourceDigest(agentGRN, operatorGRN, values))

	marshalled, err := json.Marshal(map[string]any{
		"agentGrn": agentGRN.String(), "operatorGrn": operatorGRN.String(), "values": values,
	})
	require.NoError(t, err)
	assert.NotEqual(t, agent.Digest(sha256.Sum256(marshalled)), agent.SourceDigest(agentGRN, operatorGRN, values),
		"control: encoding/json's text is not the canonical one")
}

// TestSourceDigest_CoversTheAgentTheOperatorAndEveryValue: changing any
// of the three changes the digest, where an equal source answers the
// same one.
func TestSourceDigest_CoversTheAgentTheOperatorAndEveryValue(t *testing.T) {
	a := grn.New("acme", grn.DefaultProject, agent.ServiceType, "a1")
	o := grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "o1")
	base := agent.SourceDigest(a, o, agent.Values{"model": "large"})
	assert.Equal(t, base, agent.SourceDigest(a, o, agent.Values{"model": "large"}), "control: the same source")
	assert.NotEqual(t, base, agent.SourceDigest(grn.New("acme", grn.DefaultProject, agent.ServiceType, "a2"), o, agent.Values{"model": "large"}))
	assert.NotEqual(t, base, agent.SourceDigest(a, grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "o2"), agent.Values{"model": "large"}))
	assert.NotEqual(t, base, agent.SourceDigest(a, o, agent.Values{"model": "small"}))
	assert.NotEqual(t, base, agent.SourceDigest(a, o, agent.Values{}))
}

// TestReadStoredValues_ReportsWhatDoesNotParseWithoutFailing: a payload
// garam would refuse today is listed by key and digest and left
// unparsed, where one it accepts parses.
func TestReadStoredValues_ReportsWhatDoesNotParseWithoutFailing(t *testing.T) {
	parsed := agent.ReadStoredValues([]byte(`{"model":"large"}`))
	assert.Equal(t, agent.Values{"model": "large"}, parsed.Values, "control: a payload ParseValues accepts")
	assert.Equal(t, []string{"model"}, parsed.Keys)
	assert.Equal(t, agent.ValueDigest("large").String(), parsed.ValueSHA256["model"])

	for name, stored := range map[string]string{
		"a key garam refuses":  `{"tools.pins.git":"v2"}`,
		"a value not a string": `{"replicas":2}`,
		"no object":            `["model"]`,
	} {
		t.Run(name, func(t *testing.T) {
			got := agent.ReadStoredValues([]byte(stored))
			assert.Nil(t, got.Values)
		})
	}
	mixed := agent.ReadStoredValues([]byte(`{"replicas":2,"model":"large"}`))
	assert.Equal(t, []string{"model", "replicas"}, mixed.Keys)
	assert.Equal(t, agent.ValueDigest("2").String(), mixed.ValueSHA256["replicas"], "a non-string is digested over its JSON text")
}

func TestParseDigest_AcceptsOnlyLowercaseHex(t *testing.T) {
	d := agent.SourceDigest(grn.GRN{}, grn.GRN{}, agent.Values{})
	got, err := agent.ParseDigest(d.String())
	require.NoError(t, err, "control: the form String writes")
	assert.Equal(t, d, got)
	for _, s := range []string{"", "AB", d.String()[:62], "G" + d.String()[1:], "A" + d.String()[1:]} {
		_, err := agent.ParseDigest(s)
		assert.Error(t, err, "%q", s)
	}
}
