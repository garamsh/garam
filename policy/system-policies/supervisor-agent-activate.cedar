// `iam:agent.activate` for the agent a supervisor holds, and for no
// other (ADR-0164). A guardrail of the agent's organization forbids it
// as it does any principal's.
permit (
  principal is Iam::Supervisor,
  action == Iam::Action::"agent.activate",
  resource
) when {
  principal has agent &&
  principal.agent == resource
};
