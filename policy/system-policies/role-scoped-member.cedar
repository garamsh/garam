// The member role over one team or project and nothing inside it:
// `resource in` would reach a team's member agents (ADR-0137).
permit (
  principal in ?principal,
  action in Action::"role-member",
  resource == ?resource
);
