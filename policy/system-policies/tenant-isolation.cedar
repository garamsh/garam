// A principal reaches only the organizations it is a member of, and no
// policy overrides a forbid. Membership is read from OrgMembers, which
// no team or project sits in, so being in a team of an organization is
// not being in the organization (ADR-0137).
forbid (principal, action, resource)
unless { principal in resource.org.members };
