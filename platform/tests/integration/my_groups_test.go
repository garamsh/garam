package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/wire"
)

// group creates a group of acme holding members and answers its GRN.
func (s *explainStack) group(t *testing.T, slug string, members ...grn.GRN) grn.GRN {
	t.Helper()
	g := grn.New("acme", grn.DefaultProject, org.ServiceTypeGroup, slug)
	require.NoError(t, s.orgs.CreateGroup(context.Background(), org.NewGroup(g, slug, s.acme)))
	for _, m := range members {
		require.NoError(t, s.orgs.AddGroupMember(context.Background(), g, m))
	}
	return g
}

// groupsListed lists acme's groups as cookie with query, answering the
// GRNs in the order answered.
func (s *explainStack) groupsListed(t *testing.T, cookie *http.Cookie, query url.Values) []string {
	t.Helper()
	status, body := s.get(t, "/orgs/acme/groups", query, cookie)
	require.Equal(t, http.StatusOK, status, body)
	var groups []apigen.Group
	require.NoError(t, json.Unmarshal([]byte(body), &groups))
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Grn)
	}
	return out
}

var mine = url.Values{"member": {"me"}}

// TestListGroups_MemberMeAnswersTheCallersOwnGroups lists acme's groups
// with member=me as three callers: ada belongs to eng and ops, bob to
// eng, carol to none. Each is answered its own groups and no other; the
// unfiltered list, which holds all three, is the control that the
// filter is what narrows it.
func TestListGroups_MemberMeAnswersTheCallersOwnGroups(t *testing.T) {
	s := newExplainStack(t)
	ops := s.group(t, "ops", s.adaGRN)
	qa := s.group(t, "qa")

	assert.Equal(t, []string{s.eng.String(), ops.String(), qa.String()}, s.groupsListed(t, s.carol, nil),
		"control: unfiltered, every group")
	assert.Equal(t, []string{s.eng.String(), ops.String()}, s.groupsListed(t, s.ada, mine))
	assert.Equal(t, []string{s.eng.String()}, s.groupsListed(t, s.bob, mine))
	assert.Empty(t, s.groupsListed(t, s.carol, mine), "a caller in no group belongs to none")
}

// TestListGroups_MemberMePages reads ada's three groups two at a time:
// the pages together are her groups in GRN order, and a group she is
// not in, sorting between them, appears on neither.
func TestListGroups_MemberMePages(t *testing.T) {
	s := newExplainStack(t)
	s.group(t, "fin")
	ops := s.group(t, "ops", s.adaGRN)
	qa := s.group(t, "qa", s.adaGRN)

	page := url.Values{"member": {"me"}, "limit": {"2"}}
	assert.Equal(t, []string{s.eng.String(), ops.String()}, s.groupsListed(t, s.ada, page))
	page.Set("offset", "2")
	assert.Equal(t, []string{qa.String()}, s.groupsListed(t, s.ada, page))
}

// TestListGroups_MemberMeIsReadOnGroupRead denies group:read to bob:
// member=me refuses him as the unfiltered list does. His answer before
// the Deny is the control, and the owner's after it shows the Deny
// reached bob alone. A member value other than me is refused.
func TestListGroups_MemberMeIsReadOnGroupRead(t *testing.T) {
	s := newExplainStack(t)
	require.Equal(t, []string{s.eng.String()}, s.groupsListed(t, s.bob, mine), "control, before the Deny")

	denied := activePolicy(t, s.policies, s.acme, "no-group-read",
		statementDoc("no-group-read", wire.EffectDeny, org.ActionGroupRead, s.acme.String()))
	attachPolicy(t, s.policies, denied, s.bobGRN)

	status, body := s.get(t, "/orgs/acme/groups", mine, s.bob)
	assert.Equal(t, http.StatusForbidden, status, body)
	status, body = s.get(t, "/orgs/acme/groups", nil, s.bob)
	assert.Equal(t, http.StatusForbidden, status, "the unfiltered list refuses him the same way: %s", body)
	assert.Equal(t, []string{s.eng.String()}, s.groupsListed(t, s.ada, mine), "the owner, whom the Deny does not reach")

	status, body = s.get(t, "/orgs/acme/groups", url.Values{"member": {s.adaGRN.String()}}, s.ada)
	assert.Equal(t, http.StatusBadRequest, status, "member names the caller alone: %s", body)
}
