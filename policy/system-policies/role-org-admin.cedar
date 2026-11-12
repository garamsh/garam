// The admin role over an organization and all it contains.
permit (
  principal in ?principal,
  action in Iam::Action::"role-admin",
  resource in ?resource
);
