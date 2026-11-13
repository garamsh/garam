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
func (s *Sender) SendLinks(ctx context.Context, links []policyfeed.Link) error {
	for _, l := range links {
		if err := s.sendLink(ctx, l); err != nil {
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
