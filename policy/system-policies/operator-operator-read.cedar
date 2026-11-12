// `iam:operator.read` delegated to an operator of the resource's
// organization (ADR-0146). A guardrail of that organization forbids
// it as it does any principal's.
permit (
  principal is Iam::Operator,
  action == Iam::Action::"operator.read",
  resource
) when {
  principal has operations &&
  principal.operations.contains("iam:operator.read") &&
  principal.org == resource.org
};
