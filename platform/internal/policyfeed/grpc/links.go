package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// roles is every role a link may hold, in the order a member's links
// are written and removed.
var roles = []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember}

// SendLinks writes each link's role and removes the other two, one
// policy of the policy service per role (ADR-0146 item 4). The policy
// is named by the member and the role, so a link written again is
// found there (ALREADY_EXISTS) and a link removed again is not
// (NOT_FOUND), and neither is a fault. The role is written before the
// others go, so a member whose role changes is never left with none.
// No writer is named: the service takes the caller `platform` alone to
// write a role link of the three roles with none (ADR-0146
// §Security).
//
// A link the service refuses with a code in permanentCodes is not
// retried: the member's other links are removed, so a refused write
// leaves the member with none and not with the role the link held
// before, and the link is returned as set aside while the links after
// it are sent. Any other failure, a refusal to remove included, fails
// the call and the pass is sent again.
func (s *Sender) SendLinks(
	ctx context.Context, links []policyfeed.Link,
) ([]policyfeed.LinkRefusal, error) {
	var refused []policyfeed.LinkRefusal
	for _, l := range links {
		err := s.sendLink(ctx, l)
		switch {
		case err == nil:
		case permanent(err):
			if err := s.denyLink(ctx, l); err != nil {
				return nil, err
			}
			refused = append(refused, policyfeed.LinkRefusal{
				Link: l, Code: status.Code(err).String(), Reason: err.Error(),
			})
		default:
			return nil, err
		}
	}
	return refused, nil
}

// permanentCodes is the closed list of codes the policy service answers
// a link write with that no retry clears, each with why. Every code
// not in it, UNAVAILABLE, INTERNAL, DEADLINE_EXCEEDED,
// FAILED_PRECONDITION, UNAUTHENTICATED and NOT_FOUND among them, is a
// fault of the service or of the platform's credentials that the next
// pass may find mended.
//
// Both rest on policy/crates/server/src/status.rs `to_status`:
//
//   - PERMISSION_DENIED (lines 100 to 109, 150 to 153): the service
//     refused the caller `platform` this write, which it does where the
//     identifier is held by a policy of another kind (NoWriter, raised
//     by `authorize` in policy_write.rs), or the role, scope or
//     service is not one the caller may write for the organization.
//     The body and the identifier are a function of the membership
//     alone, so sending them again is refused again.
//   - INVALID_ARGUMENT (lines 116 to 121, 166 to 180): the service
//     found the request malformed or the policy one it refuses by its
//     own rules. The same request is judged the same.
var permanentCodes = map[codes.Code]bool{
	codes.PermissionDenied: true,
	codes.InvalidArgument:  true,
}

// permanent reports whether err is a call the service refused with a
// code in permanentCodes.
func permanent(err error) bool {
	return permanentCodes[status.Code(err)]
}

// denyLink removes every role's link of the member, so a link the
// service would not write grants nothing. The service may refuse to
// remove a link for the reason it refused to write it, which leaves
// that link as it was and is not a fault of the removal.
func (s *Sender) denyLink(ctx context.Context, l policyfeed.Link) error {
	orgGRN, err := grn.Parse(l.Org)
	if err != nil {
		return errorx.Wrap(err, "role link organization", errorx.KindInternal)
	}
	for _, role := range roles {
		if err := s.removeLink(ctx, orgGRN, l.User, role); err != nil && !permanent(err) {
			return err
		}
	}
	return nil
}

func (s *Sender) sendLink(ctx context.Context, l policyfeed.Link) error {
	var held org.Role
	if l.Role != "" {
		var err error
		if held, err = org.ParseRole(l.Role); err != nil {
			return errorx.Wrap(err, "role link role", errorx.KindInternal)
		}
	}
	orgGRN, err := grn.Parse(l.Org)
	if err != nil {
		return errorx.Wrap(err, "role link organization", errorx.KindInternal)
	}
	if held != "" {
		if err := s.writeLink(ctx, orgGRN, l.User, held); err != nil {
			return err
		}
	}
	for _, role := range roles {
		if role == held {
			continue
		}
		if err := s.removeLink(ctx, orgGRN, l.User, role); err != nil {
			return err
		}
	}
	return nil
}

// linkPolicy names the policy that holds the role of a member. The
// member's GRN is hashed, because a policy's identifier is a slug of
// at most 64 characters and a GRN is neither.
func linkPolicy(orgGRN grn.GRN, user string, role org.Role) string {
	sum := sha256.Sum256([]byte(user))
	id := "link-" + string(role) + "-" + hex.EncodeToString(sum[:16])
	return grn.New(orgGRN.Identifier, "policy", id).String()
}

func (s *Sender) writeLink(ctx context.Context, orgGRN grn.GRN, user string, role org.Role) error {
	policy := linkPolicy(orgGRN, user, role)
	body := &policyv1.PolicyBody{Body: &policyv1.PolicyBody_RoleLink{RoleLink: &policyv1.RoleLink{
		Role:      protoRole(role),
		Scope:     policyv1.RoleScope_ROLE_SCOPE_ORGANIZATION,
		Principal: &policyv1.EntityRef{Type: "Iam::User", Id: user},
		Resource:  &policyv1.EntityRef{Type: "Iam::Org", Id: orgGRN.String()},
	}}}
	err := s.call(ctx, func(ctx context.Context) error {
		_, err := s.client.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{
			Org: orgGRN.String(), Policy: policy, Name: "role link " + string(role), Body: body,
		})
		return err
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return wrapCall(err, "CreatePolicy")
	}
	err = s.call(ctx, func(ctx context.Context) error {
		_, err := s.client.ActivatePolicy(ctx,
			&policyv1.ActivatePolicyRequest{Org: orgGRN.String(), Policy: policy})
		return err
	})
	return wrapCall(err, "ActivatePolicy")
}

func (s *Sender) removeLink(ctx context.Context, orgGRN grn.GRN, user string, role org.Role) error {
	err := s.call(ctx, func(ctx context.Context) error {
		_, err := s.client.DeletePolicy(ctx, &policyv1.DeletePolicyRequest{
			Org: orgGRN.String(), Policy: linkPolicy(orgGRN, user, role),
		})
		return err
	})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return wrapCall(err, "DeletePolicy")
}

// call runs one call under the sender's deadline.
func (s *Sender) call(ctx context.Context, do func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return do(ctx)
}

func protoRole(role org.Role) policyv1.Role {
	switch role {
	case org.RoleOwner:
		return policyv1.Role_ROLE_OWNER
	case org.RoleAdmin:
		return policyv1.Role_ROLE_ADMIN
	case org.RoleMember:
		return policyv1.Role_ROLE_MEMBER
	default:
		return policyv1.Role_ROLE_UNSPECIFIED
	}
}
