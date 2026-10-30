use decision::Error;
use tonic::Status;

/// The one place a domain error becomes a gRPC status. The codes are
/// the platform's error kinds, and a status withholds what the log
/// carries.
pub(crate) fn to_status(error: Error) -> Status {
    match &error {
        Error::Evaluation(_) | Error::Entities(_) => Status::invalid_argument(error.to_string()),
        Error::EntityRead(_) | Error::PolicyRead(_) => {
            tracing::error!(error = ?error, "a store could not be read");
            Status::unavailable("the policy service could not read its stores")
        }
        Error::Schema(_)
        | Error::SystemTier(_)
        | Error::RoleLinkPrincipal { .. }
        | Error::RoleLinkScope { .. }
        | Error::Validation(_)
        | Error::Composition { .. } => {
            tracing::error!(error = ?error, "a decision could not be composed");
            Status::internal("the policy service could not decide")
        }
    }
}
