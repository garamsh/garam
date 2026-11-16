// Package grpc relays policy writes and reads to the policy service
// over its gRPC API, as the caller the connection's token names
// (ADR-0169).
package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/org"
	feedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	"github.com/garamsh/garam/platform/internal/policyrelay"
	"github.com/garamsh/garam/platform/internal/wire"
)

// Client implements policyrelay.Service over a policy service client.
type Client struct {
	client policyv1.PolicyServiceClient
}

// Compile-time check that Client satisfies policyrelay.Service.
var _ policyrelay.Service = (*Client)(nil)

// New constructs a client over client.
func New(client policyv1.PolicyServiceClient) *Client {
	return &Client{client: client}
}

// Create stores a policy's first version.
func (c *Client) Create(ctx context.Context, w policyrelay.Write, name string, body policyrelay.Body) error {
	writer, pb, err := parties(w, body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	_, err = c.client.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{
		Writer: writer, Org: w.Org.String(), Policy: policyGRN(w), Name: name, Body: pb,
	})
	return relayed(err)
}

// AppendVersion stores a further version.
func (c *Client) AppendVersion(ctx context.Context, w policyrelay.Write, body policyrelay.Body) (int32, error) {
	writer, pb, err := parties(w, body)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	resp, err := c.client.AppendPolicyVersion(ctx, &policyv1.AppendPolicyVersionRequest{
		Writer: writer, Org: w.Org.String(), Policy: policyGRN(w), Body: pb,
	})
	if err != nil {
		return 0, relayed(err)
	}
	return resp.GetVersion(), nil
}

// Activate makes the default version the policy's decision.
func (c *Client) Activate(ctx context.Context, w policyrelay.Write) error {
	writer, err := subject(w.Writer)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	_, err = c.client.ActivatePolicy(ctx, &policyv1.ActivatePolicyRequest{
		Writer: writer, Org: w.Org.String(), Policy: policyGRN(w),
	})
	return relayed(err)
}

// RollBack makes an earlier version the default.
func (c *Client) RollBack(ctx context.Context, w policyrelay.Write, version int32) error {
	writer, err := subject(w.Writer)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	_, err = c.client.RollBackPolicy(ctx, &policyv1.RollBackPolicyRequest{
		Writer: writer, Org: w.Org.String(), Policy: policyGRN(w), Version: version,
	})
	return relayed(err)
}

// Delete removes the policy.
func (c *Client) Delete(ctx context.Context, w policyrelay.Write) error {
	writer, err := subject(w.Writer)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	_, err = c.client.DeletePolicy(ctx, &policyv1.DeletePolicyRequest{
		Writer: writer, Org: w.Org.String(), Policy: policyGRN(w),
	})
	return relayed(err)
}

// Get reads a policy and its versions.
func (c *Client) Get(ctx context.Context, w policyrelay.Write) (policyrelay.Detail, error) {
	writer, err := subject(w.Writer)
	if err != nil {
		return policyrelay.Detail{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	resp, err := c.client.GetPolicy(ctx, &policyv1.GetPolicyRequest{
		Writer: writer, Org: w.Org.String(), Policy: policyGRN(w),
	})
	if err != nil {
		return policyrelay.Detail{}, relayed(err)
	}
	summary, err := policyOf(resp.GetPolicy())
	if err != nil {
		return policyrelay.Detail{}, err
	}
	detail := policyrelay.Detail{Policy: summary}
	for _, v := range resp.GetVersions() {
		body, err := bodyOf(v.GetBody())
		if err != nil {
			return policyrelay.Detail{}, err
		}
		detail.Versions = append(detail.Versions, policyrelay.Version{
			Number: v.GetVersion(), Body: body, CreatedBy: v.GetCreatedBy(),
		})
	}
	return detail, nil
}

// List reads the organization's policies.
func (c *Client) List(ctx context.Context, reader, orgGRN grn.GRN) ([]policyrelay.Policy, error) {
	who, err := subject(reader)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	resp, err := c.client.ListPolicies(ctx, &policyv1.ListPoliciesRequest{
		Writer: who, Org: orgGRN.String(),
	})
	if err != nil {
		return nil, relayed(err)
	}
	out := make([]policyrelay.Policy, len(resp.GetPolicies()))
	for i, p := range resp.GetPolicies() {
		if out[i], err = policyOf(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// relayed carries the service's refusal as the kind it answered with,
// and its message, which the service wrote for the writer. A credential
// the service does not accept is the platform's fault and not the
// caller's.
func relayed(err error) error {
	switch {
	case err == nil:
		return nil
	case status.Code(err) == codes.Unauthenticated:
		return errorx.Wrap(err, "policy service refused the platform's credential", errorx.KindUnavailable)
	default:
		return wire.ErrorOf(err)
	}
}

// policyGRN names the policy the service is asked about, which it
// takes as `grn:<org>/iam/policy/<id>`.
func policyGRN(w policyrelay.Write) string {
	return grn.New(w.Org.Identifier, "policy", w.ID).String()
}

// subject names the writer as the entity type its GRN's service
// declares.
func subject(g grn.GRN) (*policyv1.Subject, error) {
	typ, err := entityType(g)
	if err != nil {
		return nil, err
	}
	return &policyv1.Subject{Type: typ, Id: g.String()}, nil
}

func parties(w policyrelay.Write, body policyrelay.Body) (*policyv1.Subject, *policyv1.PolicyBody, error) {
	writer, err := subject(w.Writer)
	if err != nil {
		return nil, nil, err
	}
	pb, err := protoBody(body)
	if err != nil {
		return nil, nil, err
	}
	return writer, pb, nil
}

func entityType(g grn.GRN) (string, error) {
	if err := g.Validate(); err != nil {
		return "", errorx.Wrap(err, "policy relay names no GRN", errorx.KindInvalidArgument)
	}
	info, ok := grn.Types(g.Service)[g.ServiceType]
	if !ok {
		return "", errorx.New(errorx.KindInvalidArgument, "no entity type for "+g.Service+" "+g.ServiceType)
	}
	return info.Entity, nil
}

func entityRef(g grn.GRN) (*policyv1.EntityRef, error) {
	typ, err := entityType(g)
	if err != nil {
		return nil, err
	}
	return &policyv1.EntityRef{Type: typ, Id: g.String()}, nil
}

func protoBody(b policyrelay.Body) (*policyv1.PolicyBody, error) {
	switch b.Kind {
	case policyrelay.KindGuardrail:
		return &policyv1.PolicyBody{Body: &policyv1.PolicyBody_Guardrail{Guardrail: b.Text}}, nil
	case policyrelay.KindPermit:
		return &policyv1.PolicyBody{Body: &policyv1.PolicyBody_Permit{Permit: b.Text}}, nil
	case policyrelay.KindRoleLink:
		if b.Link == nil {
			return nil, errorx.New(errorx.KindInvalidArgument, "a role link carries a role link")
		}
		principal, err := entityRef(b.Link.Principal)
		if err != nil {
			return nil, err
		}
		resource, err := entityRef(b.Link.Resource)
		if err != nil {
			return nil, err
		}
		role, err := protoRole(b.Link.Role)
		if err != nil {
			return nil, err
		}
		scope, err := protoScope(b.Link.Scope)
		if err != nil {
			return nil, err
		}
		return &policyv1.PolicyBody{Body: &policyv1.PolicyBody_RoleLink{RoleLink: &policyv1.RoleLink{
			Role: role, Scope: scope, Principal: principal, Resource: resource,
		}}}, nil
	}
	return nil, errorx.New(errorx.KindInvalidArgument, "policy kind is not role-link, guardrail or permit")
}

func protoRole(r org.Role) (policyv1.Role, error) {
	switch r {
	case org.RoleOwner:
		return policyv1.Role_ROLE_OWNER, nil
	case org.RoleAdmin:
		return policyv1.Role_ROLE_ADMIN, nil
	case org.RoleMember:
		return policyv1.Role_ROLE_MEMBER, nil
	}
	return 0, errorx.New(errorx.KindInvalidArgument, "org: unknown role")
}

func protoScope(s policyrelay.Scope) (policyv1.RoleScope, error) {
	switch s {
	case policyrelay.ScopeOrganization:
		return policyv1.RoleScope_ROLE_SCOPE_ORGANIZATION, nil
	case policyrelay.ScopeTeamOrProject:
		return policyv1.RoleScope_ROLE_SCOPE_TEAM_OR_PROJECT, nil
	}
	return 0, errorx.New(errorx.KindInvalidArgument, "role link scope is not organization or team-or-project")
}

// bodyOf reads a stored body back. The service stores what the
// platform sent, so a body of none of the three kinds is the service's
// fault.
func bodyOf(pb *policyv1.PolicyBody) (policyrelay.Body, error) {
	switch b := pb.GetBody().(type) {
	case *policyv1.PolicyBody_Guardrail:
		return policyrelay.Body{Kind: policyrelay.KindGuardrail, Text: b.Guardrail}, nil
	case *policyv1.PolicyBody_Permit:
		return policyrelay.Body{Kind: policyrelay.KindPermit, Text: b.Permit}, nil
	case *policyv1.PolicyBody_RoleLink:
		l := b.RoleLink
		principal, err := grn.Parse(l.GetPrincipal().GetId())
		if err != nil {
			return policyrelay.Body{}, errorx.Wrap(err, "policy service answered a role link principal", errorx.KindInternal)
		}
		resource, err := grn.Parse(l.GetResource().GetId())
		if err != nil {
			return policyrelay.Body{}, errorx.Wrap(err, "policy service answered a role link resource", errorx.KindInternal)
		}
		scope, err := scopeOf(l.GetScope())
		if err != nil {
			return policyrelay.Body{}, err
		}
		role, err := roleOf(l.GetRole())
		if err != nil {
			return policyrelay.Body{}, err
		}
		return policyrelay.Body{Kind: policyrelay.KindRoleLink, Link: &policyrelay.RoleLink{
			Role: role, Scope: scope, Principal: principal, Resource: resource,
		}}, nil
	}
	return policyrelay.Body{}, errorx.New(errorx.KindInternal, "policy service answered a body of no kind")
}

// roleOf, scopeOf, kindOf and statusOf read the service's enums back
// exhaustively: a value the platform does not know, or one left
// unspecified, is the service's fault and never a value the spec's
// enums forbid.
func roleOf(r policyv1.Role) (org.Role, error) {
	switch r {
	case policyv1.Role_ROLE_OWNER:
		return org.RoleOwner, nil
	case policyv1.Role_ROLE_ADMIN:
		return org.RoleAdmin, nil
	case policyv1.Role_ROLE_MEMBER:
		return org.RoleMember, nil
	case policyv1.Role_ROLE_UNSPECIFIED:
	}
	return "", errorx.New(errorx.KindInternal, "policy service answered a role link of no role")
}

func scopeOf(s policyv1.RoleScope) (policyrelay.Scope, error) {
	switch s {
	case policyv1.RoleScope_ROLE_SCOPE_ORGANIZATION:
		return policyrelay.ScopeOrganization, nil
	case policyv1.RoleScope_ROLE_SCOPE_TEAM_OR_PROJECT:
		return policyrelay.ScopeTeamOrProject, nil
	case policyv1.RoleScope_ROLE_SCOPE_UNSPECIFIED:
	}
	return "", errorx.New(errorx.KindInternal, "policy service answered a role link of no scope")
}

func kindOf(k policyv1.PolicyKind) (policyrelay.Kind, error) {
	switch k {
	case policyv1.PolicyKind_POLICY_KIND_ROLE_LINK:
		return policyrelay.KindRoleLink, nil
	case policyv1.PolicyKind_POLICY_KIND_GUARDRAIL:
		return policyrelay.KindGuardrail, nil
	case policyv1.PolicyKind_POLICY_KIND_PERMIT:
		return policyrelay.KindPermit, nil
	case policyv1.PolicyKind_POLICY_KIND_UNSPECIFIED:
	}
	return "", errorx.New(errorx.KindInternal, "policy service answered a policy of no kind")
}

func statusOf(s policyv1.PolicyStatus) (policyrelay.Status, error) {
	switch s {
	case policyv1.PolicyStatus_POLICY_STATUS_DRAFT:
		return policyrelay.StatusDraft, nil
	case policyv1.PolicyStatus_POLICY_STATUS_ACTIVE:
		return policyrelay.StatusActive, nil
	case policyv1.PolicyStatus_POLICY_STATUS_UNSPECIFIED:
	}
	return "", errorx.New(errorx.KindInternal, "policy service answered a policy of no status")
}

func policyOf(p *policyv1.PolicySummary) (policyrelay.Policy, error) {
	id := p.GetPolicy()
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	kind, err := kindOf(p.GetKind())
	if err != nil {
		return policyrelay.Policy{}, err
	}
	status, err := statusOf(p.GetStatus())
	if err != nil {
		return policyrelay.Policy{}, err
	}
	return policyrelay.Policy{
		ID: id, Name: p.GetName(), Kind: kind, Status: status,
		DefaultVersion: p.GetDefaultVersion(),
	}, nil
}
