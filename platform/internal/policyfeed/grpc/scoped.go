package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// SendScopedLinks writes the link of each role a group or a project
// holds and removes the link of each role it does not, one policy of
// the policy service per role (ADR-0162). The policy is named by the
// role and the owner (scopedPolicy), so a role removed is found by its
// name and needs no record of what was written before, a group or a
// project deleted removes all three, and a link written or removed
// again is no fault, as SendLinks's. A link the service refuses with a
// code in permanentCodes is set aside after the owner's links are
// removed, as a member's is.
func (s *Sender) SendScopedLinks(
	ctx context.Context, links []policyfeed.ScopedLink,
) ([]policyfeed.ScopedLinkRefusal, error) {
	var refused []policyfeed.ScopedLinkRefusal
	for _, l := range links {
		err := s.sendScopedLink(ctx, l)
		switch {
		case err == nil:
		case permanent(err):
			if err := s.denyScopedLink(ctx, l); err != nil {
				return nil, err
			}
			refused = append(refused, policyfeed.ScopedLinkRefusal{
				Link: l, Code: status.Code(err).String(), Reason: err.Error(),
			})
		default:
			return nil, err
		}
	}
	return refused, nil
}

// denyScopedLink removes every role's link of the owner, as denyLink
// does a member's.
func (s *Sender) denyScopedLink(ctx context.Context, l policyfeed.ScopedLink) error {
	for _, role := range roles {
		orgGRN, policy, err := scopedPolicy(l.Owner, role)
		if err != nil {
			return err
		}
		if err := s.removePolicy(ctx, orgGRN, policy); err != nil && !permanent(err) {
			return err
		}
	}
	return nil
}

func (s *Sender) sendScopedLink(ctx context.Context, l policyfeed.ScopedLink) error {
	held := make(map[org.Role]bool, len(l.Roles))
	for _, name := range l.Roles {
		role, err := org.ParseRole(name)
		if err != nil {
			return errorx.Wrap(err, "scoped role link role", errorx.KindInternal)
		}
		held[role] = true
	}
	typ, err := scopedEntityType(l.Owner)
	if err != nil {
		return err
	}
	for _, role := range roles {
		if !held[role] {
			continue
		}
		orgGRN, policy, err := scopedPolicy(l.Owner, role)
		if err != nil {
			return err
		}
		ref := &policyv1.EntityRef{Type: typ, Id: l.Owner}
		err = s.writePolicy(ctx, orgGRN, policy, "scoped role link "+string(role),
			&policyv1.RoleLink{
				Role:      protoRole(role),
				Scope:     policyv1.RoleScope_ROLE_SCOPE_TEAM_OR_PROJECT,
				Principal: ref,
				Resource:  ref,
			})
		if err != nil {
			return err
		}
	}
	for _, role := range roles {
		if held[role] {
			continue
		}
		orgGRN, policy, err := scopedPolicy(l.Owner, role)
		if err != nil {
			return err
		}
		if err := s.removePolicy(ctx, orgGRN, policy); err != nil {
			return err
		}
	}
	return nil
}

// scopedEntityType is the entity type of the group or the project
// named by owner, which is its own principal and its own resource.
func scopedEntityType(owner string) (string, error) {
	g, err := grn.Parse(owner)
	if err != nil {
		return "", errorx.Wrap(err, "scoped role link owner", errorx.KindInternal)
	}
	switch g.ServiceType {
	case "group":
		return "Iam::Team", nil
	case "project":
		return "Iam::Project", nil
	}
	return "", errorx.New(errorx.KindInternal, "scoped role link owner is no group or project")
}

// scopedPolicy names the policy that holds the role of a group or a
// project, and the organization it is written in.
func scopedPolicy(owner string, role org.Role) (grn.GRN, string, error) {
	g, err := grn.Parse(owner)
	if err != nil {
		return grn.GRN{}, "", errorx.Wrap(err, "scoped role link owner", errorx.KindInternal)
	}
	typ, err := scopedEntityType(owner)
	if err != nil {
		return grn.GRN{}, "", err
	}
	orgGRN := grn.Org(g.Organization)
	id := scopedPolicyID(string(role), typ, owner, typ, owner)
	return orgGRN, grn.New(g.Organization, "policy", id).String(), nil
}

// scopedPolicyID is the slug of the policy that holds a scoped link
// (ADR-0163 item 2): `link-scoped-<role>-<hex>`, where <hex> is the
// first 16 bytes of the SHA-256 of the role word, the principal's type
// and id, and the resource's type and id. Each field is written as its
// length in 8 bytes, big-endian, and then its bytes, so no two sets of
// fields share an input. The policy service reserves the identifier to
// the platform (ADR-0163 item 1).
func scopedPolicyID(role, principalType, principalID, resourceType, resourceID string) string {
	h := sha256.New()
	for _, field := range []string{role, principalType, principalID, resourceType, resourceID} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		h.Write(length[:])
		h.Write([]byte(field))
	}
	return "link-scoped-" + role + "-" + hex.EncodeToString(h.Sum(nil)[:16])
}
