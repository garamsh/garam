/// Why a policy was not written or read.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The guardrail's text is not one Cedar policy.
    #[error("the guardrail does not parse")]
    GuardrailSyntax(#[source] Box<dyn std::error::Error + Send + Sync>),
    /// An organization only narrows the system tier (ADR-0130 §4).
    #[error("a guardrail is a forbid, and this one permits")]
    GuardrailPermits,
    /// The guardrail is past what parsing it on a task's stack bears.
    #[error("the guardrail is too long or nested too deeply")]
    GuardrailTooDeep,
    /// The permit's text is not one Cedar policy.
    #[error("the permit does not parse")]
    PermitSyntax(#[source] Box<dyn std::error::Error + Send + Sync>),
    /// A permit body is a `permit`.
    #[error("a permit is a permit, and this one forbids")]
    PermitForbids,
    /// The permit is past what parsing it on a task's stack bears.
    #[error("the permit is too long or nested too deeply")]
    PermitTooDeep,
    /// Its resource scope could name a resource of any organization.
    #[error("the permit's resource scope names no entity of the organization")]
    PermitResourceUnscoped,
    /// Its resource scope is a type alone, which every organization has.
    #[error("the permit's resource scope is `is {0}` alone, which any organization holds")]
    PermitResourceByType(String),
    /// `iam:policy-permit` could not be evaluated for the writer.
    #[error("iam:policy-permit could not be evaluated")]
    PermitEvaluation(#[source] decision::Error),
    /// The engine denied the writer `iam:policy-permit`.
    #[error("{writer} may not write a permit: iam:policy-permit is denied")]
    PermitDenied {
        /// The writer's GRN.
        writer: String,
        /// The decision, with its reasons.
        decision: decision::Decision,
    },
    /// The permit was authorized for another organization or writer.
    #[error("the permit was authorized for another organization or writer")]
    PermitRight,
    /// Making a `permit` version effective needs the right to write one.
    #[error("{0} is a permit: making it effective needs a PermitRight")]
    PermitNeedsRight(String),
    /// The policy names an entity of another organization.
    #[error("{0} is not this organization's")]
    ForeignEntity(String),
    /// The GRN is not a policy of this organization.
    #[error("{0} is not a policy of this organization")]
    ForeignPolicy(String),
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
