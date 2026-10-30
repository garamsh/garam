// The owner role over an organization and all it contains.
permit (
  principal in ?principal,
  action in Action::"role-owner",
  resource in ?resource
);
