use crate::body::BodyKind;

/// Which write is made, since it picks the action the store records
/// and the writer is asked.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Step {
    /// A policy and its first version.
    Create,
    /// A version appended.
    Append,
    /// A policy made to decide.
    Activate,
    /// The default moved to another version.
    RollBack,
    /// A policy and its versions removed.
    Remove,
}

/// The action the manifest names for a step on a kind. A role link is
/// attached and detached. A `permit`'s removal only narrows, so it
/// takes the deletion every policy takes, and every other step of a
/// `permit` is the right to write one, `iam:permit.write`.
pub const fn action_for(kind: BodyKind, step: Step) -> &'static str {
    match (kind, step) {
        (BodyKind::RoleLink, Step::Remove) => "iam:policy.detach",
        (BodyKind::RoleLink, _) => "iam:policy.attach",
        (BodyKind::Guardrail, Step::Create) => "iam:policy.create",
        (BodyKind::Guardrail | BodyKind::Permit, Step::Remove) => "iam:policy.delete",
        (BodyKind::Guardrail, _) => "iam:policy.update",
        (BodyKind::Permit, _) => "iam:permit.write",
    }
}
