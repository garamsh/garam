/// Why a policy was not written or read.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The guardrail's text is not one Cedar policy.
    #[error("the guardrail does not parse")]
    GuardrailSyntax(#[source] Box<dyn std::error::Error + Send + Sync>),
    /// An organization only narrows the system tier (ADR-0130 §4).
    #[error("a guardrail is a forbid, and this one permits")]
    GuardrailPermits,
    /// The policy does not fit the schema or the template sets.
    #[error(transparent)]
    Refused(decision::Error),
    /// No policy has this GRN.
    #[error("no policy {0}")]
    NotFound(String),
    /// A policy already has this GRN.
    #[error("policy {0} already exists")]
    AlreadyExists(String),
    /// The policy holds no such version.
    #[error("policy {policy} holds no version {version}")]
    NoSuchVersion {
        /// The policy's GRN.
        policy: String,
        /// The version asked for.
        version: i32,
    },
    /// A stored row is not what this code writes.
    #[error("policy {policy} holds a body that does not read")]
    StoredBody {
        /// The policy's GRN.
        policy: String,
        /// Why it does not.
        #[source]
        source: Box<dyn std::error::Error + Send + Sync>,
    },
    /// The store failed.
    #[error("the policy store failed")]
    Store(#[source] sqlx::Error),
}
