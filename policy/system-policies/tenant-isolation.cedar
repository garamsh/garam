// A principal reaches only the organizations it is a member of, and no
// policy overrides a forbid. Membership is read from OrgMembers, which
// no team or project sits in, so being in a team of an organization is
// not being in the organization (ADR-0137).
//
// The one exception is a send between two agents of two organizations
// that each hold the other's half of consent (ADR-0147). It lifts this
// forbid only: the send still needs a permit.
forbid (principal, action, resource)
unless {
  principal in resource.org.members ||
  (action == Iam::Action::"message.send" &&
   principal is Iam::Agent && resource is Iam::Agent &&
   principal.org has consents && resource.org has consents &&
   principal.org.consents.contains(resource.org) &&
   resource.org.consents.contains(principal.org))
};
