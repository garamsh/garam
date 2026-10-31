use decision::Error;
use tonic::Status;

use crate::caller::Refusal;

/// Everything a handler can fail with.
#[derive(Debug, thiserror::Error)]
pub(crate) enum ServiceError {
    /// The caller did not authenticate.
    #[error("the caller is not authenticated")]
    Unauthenticated(#[source] Refusal),
    /// A question lacks a field neither it nor the request states.
    #[error("{0} is required")]
    Missing(&'static str),
    /// A caller sent grounds a decision reads for itself (ADR-0130 §8).
    #[error("{0} is not accepted")]
    Grounds(&'static str),
    /// A batch past the bound.
    #[error("a request holds at most {max} evaluations")]
    TooManyEvaluations {
        /// The bound.
        max: usize,
    },
    /// The domain refused or failed.
    #[error(transparent)]
    Decision(#[from] Error),
}

impl ServiceError {
    /// Whether the fault is in the question, so a batch answers it in
    /// that entry and goes on. A store failure is no such thing: it
    /// fails the call, as `READ_CURRENT` promises.
    pub(crate) fn is_the_questions(&self) -> bool {
        matches!(
            self,
            Self::Missing(_)
                | Self::Grounds(_)
                | Self::Decision(Error::Evaluation(_) | Error::Entities(_))
        )
    }
}

/// The one place an error becomes a gRPC status. The codes are the
/// platform's error kinds, and what a status withholds the log carries.
pub(crate) fn to_status(error: &ServiceError) -> Status {
    match error {
        ServiceError::Unauthenticated(_) => {
            tracing::info!(error = ?error, "refused a caller");
            Status::unauthenticated(error.to_string())
        }
        ServiceError::Missing(_)
        | ServiceError::Grounds(_)
        | ServiceError::TooManyEvaluations { .. }
        | ServiceError::Decision(Error::Evaluation(_) | Error::Entities(_)) => {
            tracing::info!(error = ?error, "refused a question");
            Status::invalid_argument(error.to_string())
        }
        ServiceError::Decision(Error::EntityRead(_) | Error::PolicyRead(_)) => {
            tracing::error!(error = ?error, "a store could not be read");
            Status::unavailable("the policy service could not read its stores")
        }
        ServiceError::Decision(
            Error::Schema(_)
            | Error::SystemTier(_)
            | Error::RoleLinkPrincipal { .. }
            | Error::RoleLinkScope { .. }
            | Error::Validation(_)
            | Error::Composition { .. },
        ) => {
            tracing::error!(error = ?error, "a decision could not be composed");
            Status::internal("the policy service could not decide")
        }
    }
}
