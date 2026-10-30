use cedar_policy::{AuthorizationError, Decision as CedarDecision, PolicyId, Response};

/// The answer to one evaluation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Outcome {
    /// The subject may perform the action.
    Allow,
    /// The subject may not.
    Deny,
}

/// One ground of a decision.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Reason {
    /// A policy that decided.
    Policy(PolicyId),
    /// A policy that failed to evaluate.
    Error {
        /// The policy the error arose in.
        policy: PolicyId,
        /// What went wrong.
        message: String,
    },
}

/// An outcome with the reasons for it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Decision {
    /// The answer.
    pub outcome: Outcome,
    /// What decided it, and every error when one arose.
    pub reasons: Vec<Reason>,
}

impl Decision {
    /// Reads Cedar's response, failing closed: Cedar skips a policy
    /// that errors, so a `forbid` that errored would leave its permit
    /// standing.
    pub(crate) fn from_response(response: &Response) -> Self {
        let errors: Vec<Reason> = response
            .diagnostics()
            .errors()
            .map(|error| match error {
                AuthorizationError::PolicyEvaluationError(failure) => Reason::Error {
                    policy: failure.policy_id().clone(),
                    message: failure.to_string(),
                },
            })
            .collect();
        if !errors.is_empty() {
            return Self {
                outcome: Outcome::Deny,
                reasons: errors,
            };
        }
        Self {
            outcome: match response.decision() {
                CedarDecision::Allow => Outcome::Allow,
                CedarDecision::Deny => Outcome::Deny,
            },
            reasons: response
                .diagnostics()
                .reason()
                .cloned()
                .map(Reason::Policy)
                .collect(),
        }
    }
}
