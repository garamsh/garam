package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/org"
)

// TestScopedPolicy_IsTheIdentifierADR0163Holds pins the Go computation
// to the two identifiers ADR-0163 item 2 states as literals, which the
// policy service's own computation is held to as well: the platform and
// the service must derive the same name, or the service refuses the
// write as the write of a reserved identifier by a stranger.
func TestScopedPolicy_IsTheIdentifierADR0163Holds(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		role  org.Role
		want  string
	}{
		{"a team's admin link", "grn:acme/iam/group/eng", org.RoleAdmin,
			"grn:acme/iam/policy/link-scoped-admin-057a36867e8b5d54bdfba284ee2f3a02"},
		{"a project's member link", "grn:acme/iam/project/web", org.RoleMember,
			"grn:acme/iam/policy/link-scoped-member-0e9bfafcc0092e41678c97122d4c76be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgGRN, policy, err := scopedPolicy(tt.owner, tt.role)
			require.NoError(t, err)
			assert.Equal(t, tt.want, policy)
			assert.Equal(t, "grn:root/iam/org/acme", orgGRN.String())
		})
	}
}

// TestScopedPolicyID_FieldsAreCutByLength holds that a byte moved from
// one field to the next is another input: without the length before
// each field, "ab"+"c" and "a"+"bc" would hash alike.
func TestScopedPolicyID_FieldsAreCutByLength(t *testing.T) {
	a := scopedPolicyID("admin", "Iam::Team", "ab", "c", "")
	b := scopedPolicyID("admin", "Iam::Team", "a", "bc", "")
	assert.NotEqual(t, a, b)
}

// TestScopedPolicy_RoleAndOwnerKindEachChangeTheName holds that the
// three roles of one owner are three policies, and that a group and a
// project of one slug are two.
func TestScopedPolicy_RoleAndOwnerKindEachChangeTheName(t *testing.T) {
	names := map[string]bool{}
	for _, owner := range []string{"grn:acme/iam/group/x", "grn:acme/iam/project/x"} {
		for _, role := range roles {
			_, policy, err := scopedPolicy(owner, role)
			require.NoError(t, err)
			names[policy] = true
		}
	}
	assert.Len(t, names, 6)
}

func TestScopedPolicy_RefusesAnOwnerThatIsNoGroupOrProject(t *testing.T) {
	_, _, err := scopedPolicy("grn:acme/iam/user/lee", org.RoleAdmin)
	assert.Error(t, err)
}
