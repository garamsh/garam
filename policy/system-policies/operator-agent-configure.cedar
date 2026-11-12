// `iam:agent.configure` delegated to an operator of the resource's
// organization (ADR-0146). A guardrail of that organization forbids
// it as it does any principal's.
permit (
  principal is Iam::Operator,
  action == Iam::Action::"agent.configure",
  resource
) when {
  principal has operations &&
  principal.operations.contains("iam:agent.configure") &&
  principal.org == resource.org
};
