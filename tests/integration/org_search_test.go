package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/wire"
)

// foundKind is one kind of searchOrg's answer: the GRNs it found in
// order, each one's name where it has one, and how many more matched.
type foundKind struct {
	grns  []string
	names map[string]*string
	more  int
}

// search asks searchOrg as cookie and answers each kind it found.
func (s *listStack) search(t *testing.T, q string, extra url.Values, cookie *http.Cookie) map[string]foundKind {
	t.Helper()
	query := url.Values{"q": {q}}
	for k, v := range extra {
		query[k] = v
	}
	status, body := s.get(t, "/orgs/acme/search", query, cookie)
	require.Equal(t, http.StatusOK, status, body)
	var answer struct {
		Found []struct {
			Kind    string `json:"kind"`
			Objects []struct {
				Grn  string  `json:"grn"`
				Name *string `json:"name"`
			} `json:"objects"`
			More int `json:"more"`
		} `json:"found"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &answer))
	out := map[string]foundKind{}
	for _, k := range answer.Found {
		f := foundKind{names: map[string]*string{}, more: k.More, grns: []string{}}
		for _, o := range k.Objects {
			f.grns = append(f.grns, o.Grn)
			f.names[o.Grn] = o.Name
		}
		out[k.Kind] = f
	}
	return out
}

// searchSeed creates one object of a kind searchOrg finds, named name,
// and answers its GRN. id is its identifier where the kind lets the
// caller choose one, and ignored where garam draws it.
type searchSeed func(t *testing.T, s *listStack, id, name string) grn.GRN

func seedNamedGroup(t *testing.T, s *listStack, id, name string) grn.GRN {
	t.Helper()
	g := grn.New("acme", grn.DefaultProject, org.ServiceTypeGroup, id)
	require.NoError(t, s.orgs.CreateGroup(context.Background(), org.NewGroup(g, name, s.acme)))
	return g
}

func seedNamedProject(t *testing.T, s *listStack, id, name string) grn.GRN {
	t.Helper()
	g := grn.New("acme", grn.DefaultProject, org.ServiceTypeProject, id)
	require.NoError(t, s.orgs.CreateProject(context.Background(), org.NewProject(g, name, s.acme, string(org.RoleAdmin))))
	return g
}

func seedNamedMember(t *testing.T, s *listStack, id, nickname string) grn.GRN {
	t.Helper()
	g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
	require.NoError(t, userpg.New(s.pool).Create(context.Background(), user.New(g, nickname)))
	require.NoError(t, s.orgs.AddMember(context.Background(), org.NewMembership(s.acme, g, org.RoleMember)))
	return g
}

// seedNamedAgent keeps a definition naming a new agent, whose
// identifier garam draws.
func seedNamedAgent(t *testing.T, s *listStack, _, name string) grn.GRN {
	t.Helper()
	operatorGRN := registerOperator(t, s.pool, scopedGRN("acme", agent.ServiceTypeOperator, "controller"))
	d, err := agent.NewDefinition(s.acme, operatorGRN, agent.Name(name), agent.Values{"model": "haiku"},
		scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	keepDefinition(t, s.pool, d)
	return d.Agent()
}

// seedNamedPolicy creates an active policy, whose identifier garam
// draws.
func seedNamedPolicy(t *testing.T, s *listStack, _, name string) grn.GRN {
	t.Helper()
	g, err := grn.Parse(activePolicy(t, s.policies, s.acme, name,
		statementDoc("allow", wire.EffectAllow, org.ActionOrgRead, s.acme.String())))
	require.NoError(t, err)
	return g
}

// TestSearch_FindsEachKindByNameOrIdentifierCaseAside seeds, for each
// kind, one object whose name holds "ops" in another case, one whose
// identifier holds it where the kind lets the caller choose one, and
// one whose neither does. "ops" finds the first two and not the third,
// and the third's own identifier is the control that it is findable
// at all: what left it out was the needle.
func TestSearch_FindsEachKindByNameOrIdentifierCaseAside(t *testing.T) {
	kinds := []struct {
		kind       string
		seed       searchSeed
		choosesIDs bool
	}{
		{"person", seedNamedMember, true},
		{"agent", seedNamedAgent, false},
		{"team", seedNamedGroup, true},
		{"policy", seedNamedPolicy, false},
		{"project", seedNamedProject, true},
	}
	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			s := newListStack(t)
			byName := k.seed(t, s, "k-1", "DevOps One")
			want := []string{byName.String()}
			if k.choosesIDs {
				byID := k.seed(t, s, "ops-2", "Platform")
				want = append(want, byID.String())
			}
			other := k.seed(t, s, "k-3", "Sales")

			found := s.search(t, "ops", nil, s.ada)
			assert.ElementsMatch(t, want, found[k.kind].grns, "the name or the identifier holds the needle, case aside")
			assert.Equal(t, "DevOps One", *found[k.kind].names[byName.String()], "an object answers its name")
			assert.Zero(t, found[k.kind].more)

			found = s.search(t, other.Identifier, nil, s.ada)
			assert.Contains(t, found[k.kind].grns, other.String(), "control: the third is found by its own identifier")
		})
	}
}

// TestSearch_AnAgentNoDefinitionNamesIsFoundByIdentifierWithoutAName
// reads an agent that reported but was never defined: it is found by
// its identifier and answers no name, after the defined agents.
func TestSearch_AnAgentNoDefinitionNamesIsFoundByIdentifierWithoutAName(t *testing.T) {
	s := newListStack(t)
	undefined := seedAgent(t, s.agents, s.acme)
	defined := seedNamedAgent(t, s, "", "Agent "+undefined.Identifier)

	found := s.search(t, undefined.Identifier, nil, s.ada)["agent"]
	assert.Equal(t, []string{defined.String(), undefined.String()}, found.grns, "the defined agent first")
	assert.Nil(t, found.names[undefined.String()], "no definition names it")
	assert.Equal(t, "Agent "+undefined.Identifier, *found.names[defined.String()])
}

// TestSearch_AnswersLimitOfEachKindAndCountsTheRest seeds seven teams
// that match: the default answers five and counts two more, a limit of
// two answers the first two in identifier order and counts five, and a
// limit outside 1 to 20 or a blank needle is refused.
func TestSearch_AnswersLimitOfEachKindAndCountsTheRest(t *testing.T) {
	s := newListStack(t)
	var teams []string
	for _, id := range []string{"ops-1", "ops-2", "ops-3", "ops-4", "ops-5", "ops-6", "ops-7"} {
		teams = append(teams, seedNamedGroup(t, s, id, id).String())
	}

	found := s.search(t, "ops", nil, s.ada)["team"]
	assert.Equal(t, teams[:5], found.grns, "five when no limit is asked")
	assert.Equal(t, 2, found.more)

	found = s.search(t, "ops", url.Values{"limit": {"2"}}, s.ada)["team"]
	assert.Equal(t, teams[:2], found.grns)
	assert.Equal(t, 5, found.more)

	found = s.search(t, "ops", url.Values{"limit": {"20"}}, s.ada)["team"]
	assert.Equal(t, teams, found.grns, "control: the bound itself is accepted")

	for _, q := range []url.Values{
		{"q": {"ops"}, "limit": {"0"}},
		{"q": {"ops"}, "limit": {"21"}},
		{"q": {"   "}},
	} {
		status, body := s.get(t, "/orgs/acme/search", q, s.ada)
		assert.Equal(t, http.StatusBadRequest, status, "%v: %s", q, body)
	}
}

// TestSearch_LikeWildcardsMatchOnlyThemselves seeds a team whose name
// holds each wildcard and one that holds a character the wildcard
// would stand for: "%" and "_" find only the first, and "50" and "b",
// which both hold, are the control that both are findable.
func TestSearch_LikeWildcardsMatchOnlyThemselves(t *testing.T) {
	s := newListStack(t)
	percent := seedNamedGroup(t, s, "pct-1", "50% off")
	seedNamedGroup(t, s, "pct-2", "500 off")
	underscore := seedNamedGroup(t, s, "us-1", "a_b")
	seedNamedGroup(t, s, "us-2", "axb")

	assert.Equal(t, []string{percent.String()}, s.search(t, "%", nil, s.ada)["team"].grns, "% matches only a %")
	assert.Equal(t, []string{underscore.String()}, s.search(t, "_", nil, s.ada)["team"].grns, "_ matches only a _")
	assert.Len(t, s.search(t, "50", nil, s.ada)["team"].grns, 2, "control: both percent teams are findable")
	assert.Len(t, s.search(t, "b", nil, s.ada)["team"].grns, 2, "control: both underscore teams are findable")
}

// TestSearch_ADenyOnOneObjectHidesItFromTheSearchAndItsCount seeds
// three matching objects of each kind filtered per object and denies
// ada the middle one's read. dan, whom the Deny does not reach, is the
// control: ada's answer is his without that object, and the count past
// a limit of one counts only what she may read.
func TestSearch_ADenyOnOneObjectHidesItFromTheSearchAndItsCount(t *testing.T) {
	kinds := []struct {
		kind, action string
		seed         searchSeed
	}{
		{"agent", org.ActionAgentRead, seedNamedAgent},
		{"team", org.ActionGroupRead, seedNamedGroup},
		{"policy", org.ActionPolicyRead, seedNamedPolicy},
		{"project", org.ActionProjectRead, seedNamedProject},
	}
	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			s := newListStack(t)
			first, hidden, last := k.seed(t, s, "ops-a", "ops a"), k.seed(t, s, "ops-b", "ops b"), k.seed(t, s, "ops-c", "ops c")
			require.ElementsMatch(t, []string{first.String(), hidden.String(), last.String()},
				s.search(t, "ops", nil, s.ada)[k.kind].grns, "control, before the Deny")

			deny := activePolicy(t, s.policies, s.acme, "deny-one",
				statementDoc("deny-one", wire.EffectDeny, k.action, hidden.String()))
			attachPolicy(t, s.policies, deny, s.adaGRN)

			control := s.search(t, "ops", nil, s.dan)[k.kind].grns
			require.Contains(t, control, hidden.String(), "the Deny does not reach dan")
			want := slices.DeleteFunc(slices.Clone(control), func(g string) bool { return g == hidden.String() })
			assert.Equal(t, want, s.search(t, "ops", nil, s.ada)[k.kind].grns, "ada's answer is dan's without the denied object")

			one := url.Values{"limit": {"1"}}
			assert.Equal(t, 2, s.search(t, "ops", one, s.dan)[k.kind].more, "control: dan counts both others")
			assert.Equal(t, 1, s.search(t, "ops", one, s.ada)[k.kind].more, "ada counts only what she may read")
		})
	}
}

// TestSearch_LeavesOutAKindTheCallerMayNotList denies ada the check a
// kind's list makes on the organization: that kind is left out of her
// answer, and dan's, which holds it, is the control.
func TestSearch_LeavesOutAKindTheCallerMayNotList(t *testing.T) {
	kinds := []struct {
		kind, action string
		seed         searchSeed
	}{
		{"person", org.ActionOrgMemberList, seedNamedMember},
		{"agent", org.ActionAgentRead, seedNamedAgent},
		{"team", org.ActionGroupRead, seedNamedGroup},
		{"policy", org.ActionPolicyRead, seedNamedPolicy},
		{"project", org.ActionProjectRead, seedNamedProject},
	}
	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			s := newListStack(t)
			k.seed(t, s, "ops-1", "ops one")
			deny := activePolicy(t, s.policies, s.acme, "deny-list",
				statementDoc("deny-list", wire.EffectDeny, k.action, s.acme.String()))
			attachPolicy(t, s.policies, deny, s.adaGRN)

			require.Contains(t, s.search(t, "ops", nil, s.dan), k.kind, "control: dan may list it")
			found := s.search(t, "ops", nil, s.ada)
			assert.NotContains(t, found, k.kind, "the kind ada may not list is left out")
			assert.NotEmpty(t, found, "and the rest still answer")
		})
	}
}
