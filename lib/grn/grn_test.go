package grn_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

func TestNewAndString(t *testing.T) {
	g := grn.New("garam", "user", "alice")
	assert.Equal(t, "grn:garam/iam/user/alice", g.String())
}

func TestOrg(t *testing.T) {
	assert.Equal(t, "grn:root/iam/org/acme", grn.Org("acme").String())
}

func TestIsZero(t *testing.T) {
	assert.True(t, grn.GRN{}.IsZero())
	assert.False(t, grn.New("garam", "user", "alice").IsZero())
}

// TestValidate reads the accepted rows against the rejected ones. The
// accepted ones are the segment shapes garam builds GRNs from, so a
// rule tightened past what is stored fails here rather than in
// production; the rejected ones are the characters that rename a
// resource somewhere it is carried.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		g       grn.GRN
		wantErr bool
	}{
		{"reserved owner and project", grn.New(grn.RootOwner, "org", "acme"), false},
		{"randomHex identifier", grn.New(grn.RootOwner, "org", "9f2c4b81d0e7a35c"), false},
		{"oidc subject identifier", grn.New(grn.RootOwner, "user", "117421510527410544321"), false},
		{"hyphenated slug", grn.New("acme", "agent", "bot-1"), false},
		{"identifier at the length bound", grn.New("acme", "agent", strings.Repeat("a", 64)), false},

		{"identifier past the length bound", grn.New("acme", "agent", strings.Repeat("a", 65)), true},
		{"identifier holds a question mark", grn.New("acme", "agent", "bot?1"), true},
		{"identifier holds a hash", grn.New("acme", "agent", "bot#1"), true},
		{"identifier holds an asterisk", grn.New("acme", "agent", "bot*"), true},
		{"organization holds a colon", grn.New("ac:me", "agent", "bot-1"), true},
		{"organization is uppercase", grn.New("ACME", "agent", "bot-1"), true},
		{"ancestors are whole pairs", grn.GRN{Organization: "acme", Ancestors: "project/frontend", Service: "cowork", ServiceType: "channel", Identifier: "general"}, false},
		{"locator is carried", grn.GRN{Organization: "acme", Service: "cowork", ServiceType: "page", Identifier: "home", Locator: "refs/heads/main:cmd/main.go"}, false},

		{"ancestors hold half a pair", grn.GRN{Organization: "acme", Ancestors: "project", Service: "cowork", ServiceType: "channel", Identifier: "general"}, true},
		{"ancestor holds an underscore", grn.GRN{Organization: "acme", Ancestors: "project/team_one", Service: "cowork", ServiceType: "channel", Identifier: "general"}, true},
		{"locator holds a hash", grn.GRN{Organization: "acme", Service: "cowork", ServiceType: "page", Identifier: "home", Locator: "a#b"}, true},
		{"type has no owning service", grn.New("acme", "widget", "x"), true},
		{"service type holds a slash", grn.New("acme", "agent/x", "bot-1"), true},
		{"identifier holds a slash", grn.New("acme", "agent", "bot/1"), true},
		{"identifier holds a control character", grn.New("acme", "agent", "bot\n1"), true},
		{"identifier is not ascii", grn.New("acme", "agent", "bot-é"), true},
		{"empty org", grn.New("", "user", "i"), true},
		{"empty type", grn.New("o", "", "i"), true},
		{"empty id", grn.New("o", "user", ""), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.g.Validate()
			if tc.wantErr {
				assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestValidate_KeepsAGRNWholeInAURI reads the rule against what it is
// for. A GRN is an opaque URI, so a verifier recovers the identity
// from the whole URI or from Opaque alone (ADR-0014): an accepted GRN
// comes back from both, and every rejected row is a character that
// makes at least one reading recover something else.
func TestValidate_KeepsAGRNWholeInAURI(t *testing.T) {
	accepted := map[string]grn.GRN{
		"randomHex identifier":   grn.New(grn.RootOwner, "org", "9f2c4b81d0e7a35c"),
		"nested under a project": {Organization: "acme", Ancestors: "project/frontend", Service: "cowork", ServiceType: "channel", Identifier: "general"},
		"hyphenated slug":        grn.New("acme", "agent", "bot-1"),
	}
	for name, g := range accepted {
		t.Run("accepts a "+name, func(t *testing.T) {
			require.NoError(t, g.Validate())
			whole, opaque := readings(t, g)
			assert.Equal(t, g, whole)
			assert.Equal(t, g, opaque)
		})
	}

	rejected := map[string]grn.GRN{
		"question mark": grn.New("acme", "agent", "bot?1"),
		"hash":          grn.New("acme", "agent", "bot#1"),
		"colon":         grn.New("ac:me", "agent", "bot-1"),
	}
	for name, g := range rejected {
		t.Run("rejects a "+name, func(t *testing.T) {
			assert.True(t, errorx.IsKind(g.Validate(), errorx.KindInvalidArgument))
			whole, opaque := readings(t, g)
			assert.False(t, whole == g && opaque == g,
				"both readings recover this GRN, so the row no longer shows what the rule is for")
		})
	}
}

// readings returns the GRN recovered from the whole URI and from its
// opaque part alone. A reading that does not parse comes back as the
// zero GRN, which no reading of a valid GRN yields.
func readings(t *testing.T, g grn.GRN) (whole, opaque grn.GRN) {
	t.Helper()
	u, err := url.Parse(g.String())
	require.NoError(t, err)
	parse := func(s string) grn.GRN {
		parsed, err := grn.Parse(s)
		if err != nil {
			return grn.GRN{}
		}
		return parsed
	}
	return parse(u.String()), parse(u.Scheme + ":" + u.Opaque)
}

// nameOfLength returns a valid GRN whose name before '#' is n
// characters, n between 493 and 538 (the ancestors take the slack).
func nameOfLength(t *testing.T, n int) grn.GRN {
	t.Helper()
	full := strings.Repeat("a", 64)
	g := grn.GRN{
		Organization: "acme",
		Service:      "iam",
		Ancestors: strings.Join([]string{full, full, full, full, full, full,
			strings.Repeat("b", 19), strings.Repeat("c", n-474-19)}, "/"),
		ServiceType: "user",
		Identifier:  full,
	}
	require.Len(t, g.String(), n, "setup built a name of another length")
	return g
}

// TestValidate_BoundsTheNameBeforeTheLocator reads the bound from both
// sides, and shows a locator is not counted in it.
func TestValidate_BoundsTheNameBeforeTheLocator(t *testing.T) {
	assert.NoError(t, nameOfLength(t, 512).Validate())

	over := nameOfLength(t, 513)
	assert.True(t, errorx.IsKind(over.Validate(), errorx.KindInvalidArgument))

	located := nameOfLength(t, 512)
	located.Locator = strings.Repeat("l", 600)
	assert.NoError(t, located.Validate())
}

func TestParse(t *testing.T) {
	nested := grn.GRN{Organization: "acme", Ancestors: "project/frontend", Service: "cowork", ServiceType: "channel", Identifier: "general"}
	located := grn.GRN{Organization: "acme", Service: "cowork", ServiceType: "page", Identifier: "home", Locator: "refs/heads/main:cmd/main.go"}
	tests := []struct {
		name    string
		input   string
		want    grn.GRN
		wantErr bool
	}{
		{"valid", "grn:garam/iam/user/alice", grn.New("garam", "user", "alice"), false},
		{"root organization", "grn:root/iam/org/acme", grn.Org("acme"), false},
		{"nested under a project", "grn:acme/cowork/project/frontend/channel/general", nested, false},
		{"locator", "grn:acme/cowork/page/home#refs/heads/main:cmd/main.go", located, false},
		{"escaped hash in a locator", "grn:acme/cowork/page/home#a%23b", grn.GRN{Organization: "acme", Service: "cowork", ServiceType: "page", Identifier: "home", Locator: "a%23b"}, false},
		{"five-segment form", strings.Join([]string{"grn", "garam", "default", "user", "alice"}, ":"), grn.GRN{}, true},
		{"five-segment form of a nested name", strings.Join([]string{"grn", "acme", "frontend", "repo", "api"}, ":"), grn.GRN{}, true},
		{"missing prefix", "arn:garam/iam/user/alice", grn.GRN{}, true},
		{"no pair", "grn:garam/iam", grn.GRN{}, true},
		{"half a pair", "grn:garam/iam/user", grn.GRN{}, true},
		{"odd pairs", "grn:garam/iam/project/frontend/repo", grn.GRN{}, true},
		{"empty segment", "grn:garam/iam//alice", grn.GRN{}, true},
		{"empty input", "", grn.GRN{}, true},
		{"service does not own the type", "grn:garam/cowork/user/alice", grn.GRN{}, true},
		{"type unknown", "grn:garam/iam/widget/x", grn.GRN{}, true},
		{"empty locator", "grn:acme/cowork/page/home#", grn.GRN{}, true},
		{"second hash", "grn:acme/cowork/page/home#a#b", grn.GRN{}, true},
		{"identifier holds a question mark", "grn:garam/iam/user/al?ice", grn.GRN{}, true},
		{"segment holds a colon", "grn:garam/iam/user/al:ice", grn.GRN{}, true},
		{"name past 512 characters", nameOfLength(t, 513).String(), grn.GRN{}, true},
		{"name at 512 characters", nameOfLength(t, 512).String(), nameOfLength(t, 512), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := grn.Parse(tc.input)
			if tc.wantErr {
				assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.input, got.String())
		})
	}
}

// TestParse_TellsAnUndeclaredNameFromOneThatIsNoGRN reads the refusal
// for a service or type no manifest declares apart from the refusals
// for a string that is no GRN. Each other row is the control: it is
// not ErrUndeclared, so the sentinel tracks the catalogue and not
// every refusal.
func TestParse_TellsAnUndeclaredNameFromOneThatIsNoGRN(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		undeclared bool
		refused    bool
	}{
		{"service no manifest declares", "grn:acme/nosuch/thing/x", true, true},
		{"type its service does not declare", "grn:acme/iam/widget/x", true, true},
		{"type another service declares", "grn:acme/cowork/user/alice", true, true},
		{"no prefix", "arn:acme/iam/user/alice", false, true},
		{"half a pair", "grn:acme/iam/user", false, true},
		{"five-segment form", "grn:acme:default:user:alice", false, true},
		{"invalid organization", "grn:AC/iam/user/alice", false, true},
		{"invalid identifier", "grn:acme/iam/user/al?ice", false, true},
		{"declared and well formed", "grn:acme/iam/user/alice", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := grn.Parse(tc.input)
			assert.Equal(t, tc.refused, err != nil)
			assert.Equal(t, tc.undeclared, errors.Is(err, grn.ErrUndeclared))
			if tc.refused {
				assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
			}
		})
	}
}

// TestGRN_ParentIsTheNameWithItsLastPairRemoved reads ADR-0142 item 3:
// a name of one pair has none, and a locator is not part of a parent.
func TestGRN_ParentIsTheNameWithItsLastPairRemoved(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"one pair", "grn:acme/iam/user/alice", ""},
		{"organization", "grn:root/iam/org/acme", ""},
		{"project-scoped cowork name", "grn:acme/cowork/project/frontend/channel/general", "grn:acme/iam/project/frontend"},
		{"iam name under a project", "grn:acme/iam/project/frontend/agent/bot", "grn:acme/iam/project/frontend"},
		{"nested two deep", "grn:acme/cowork/project/frontend/channel/general/task/t1", "grn:acme/cowork/project/frontend/channel/general"},
		{"locator dropped", "grn:acme/cowork/project/frontend/page/home#section-2", "grn:acme/iam/project/frontend"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := grn.Parse(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, g.Parent())
		})
	}
}

// TestGRN_EveryParentIsAGRN holds that a parent is a name garam knows:
// following it never lands on a name Parse refuses, up to a name of one
// pair.
func TestGRN_EveryParentIsAGRN(t *testing.T) {
	for _, input := range []string{
		"grn:acme/cowork/project/frontend/channel/general",
		"grn:acme/iam/project/frontend/agent/bot",
		"grn:acme/cowork/project/frontend/channel/general/task/t1",
		"grn:acme/cowork/project/frontend/page/home#section-2",
	} {
		g, err := grn.Parse(input)
		require.NoError(t, err, input)
		for parent := g.Parent(); parent != ""; parent = g.Parent() {
			g, err = grn.Parse(parent)
			require.NoError(t, err, "the parent %s of %s", parent, input)
		}
	}
}

// TestService_AgreesWithTheCatalogue reads each type's owner against
// grn-model.md §Types by owning service.
func TestService_AgreesWithTheCatalogue(t *testing.T) {
	want := map[string]string{
		"user": "iam", "agent": "iam", "org": "iam", "group": "iam",
		"project": "iam", "operator": "iam", "supervisor": "iam",
		"policy": "iam", "service": "iam",
		"page": "cowork", "channel": "cowork", "task": "cowork",
		"artifact": "cowork",
	}
	for typ, service := range want {
		assert.Equal(t, service, grn.New("acme", typ, "x").Service, typ)
	}
	assert.Empty(t, grn.New("acme", "widget", "x").Service)
}

// TestNewIn_ATypeBelongsToTheServiceThatDeclaresIt reads the service
// off the segment and not off the type: a name under a service that
// declares no such type is refused, and so is one under a service no
// manifest declares.
func TestNewIn_ATypeBelongsToTheServiceThatDeclaresIt(t *testing.T) {
	g := grn.NewIn("acme", "cowork", "page", "home")
	require.NoError(t, g.Validate())
	assert.Equal(t, "grn:acme/cowork/page/home", g.String())
	parsed, err := grn.Parse("grn:acme/cowork/page/home")
	require.NoError(t, err)
	assert.Equal(t, "cowork", parsed.Service)
	assert.Equal(t, g, parsed)

	_, err = grn.Parse("grn:acme/iam/page/home")
	require.Error(t, err, "iam holds no page")
	_, err = grn.Parse("grn:acme/billing/invoice/1")
	require.Error(t, err, "no manifest declares billing")
	repo, err := grn.Parse("grn:acme/codehub/repo/4821")
	require.NoError(t, err, "codehub declares repo")
	assert.Equal(t, "codehub", repo.Service)
}

// TestValidate_LocatorIsAnRFC3986Fragment holds the locator to the
// fragment production: pchar, '/' and '?', with '%' only as a whole
// escape.
func TestValidate_LocatorIsAnRFC3986Fragment(t *testing.T) {
	for _, loc := range []string{
		"refs/heads/main:cmd/main.go", "a%23b", "a%2fB", "x?y=1&z=2",
		"a@b", "it's", "~._-!$()*+,;=",
	} {
		g := grn.NewIn("acme", "cowork", "page", "home")
		g.Locator = loc
		assert.NoError(t, g.Validate(), loc)
		parsed, err := grn.Parse(g.String())
		require.NoError(t, err, loc)
		assert.Equal(t, g, parsed, loc)
	}
	for _, loc := range []string{
		"has space", "a%", "a%2", "a%zz", "a%2g", "tab\there", "é",
		"a\"b", "a<b", "a\\b", "a[b", "a{b", "a|b", "a^b", "a`b",
	} {
		g := grn.NewIn("acme", "cowork", "page", "home")
		g.Locator = loc
		assert.Error(t, g.Validate(), loc)
		_, err := grn.Parse("grn:acme/cowork/page/home#" + loc)
		assert.Error(t, err, loc)
	}
}

// TestOrgPattern_ScopesToTheOrganizationByPrefix reads the pattern as
// a prefix: it covers the organization's names and not another's,
// whose slug it merely starts with.
func TestOrgPattern_ScopesToTheOrganizationByPrefix(t *testing.T) {
	pattern := grn.OrgPattern("acme")
	assert.Equal(t, "grn:acme/*", pattern)

	prefix := strings.TrimSuffix(pattern, "*")
	assert.True(t, strings.HasPrefix(grn.New("acme", "user", "alice").String(), prefix))
	assert.False(t, strings.HasPrefix(grn.New("acme2", "user", "alice").String(), prefix))
}
