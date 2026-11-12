// The member role over an organization and all it contains.
permit (
  principal in ?principal,
  action in Iam::Action::"role-member",
  resource in ?resource
);
