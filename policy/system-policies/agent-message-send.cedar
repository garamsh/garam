// An agent nobody has written a policy for goes on sending inside its
// organization, and an organization narrows that with a forbid
// (ADR-0024). Tenant isolation holds the organization boundary.
permit (
  principal is Agent,
  action == Action::"message:send",
  resource is Agent
);
