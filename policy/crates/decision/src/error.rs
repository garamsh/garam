/// Why the engine refused to load or to take an input.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The embedded schema does not parse.
    #[error("the schema does not load")]
    Schema(#[source] Cause),
    /// A system policy does not parse or does not validate.
    #[error("the system tier does not load: {0}")]
    SystemTier(String),
    /// An evaluation names what the schema does not.
    #[error("the evaluation does not fit the schema")]
    Evaluation(#[source] Cause),
    /// Entities do not conform to the schema.
    #[error("the entities do not fit the schema")]
    Entities(#[source] Cause),
    /// A role link names a principal no grant can be held by.
    #[error("role link {link}: {principal} holds no role")]
    RoleLinkPrincipal {
        /// The link's id.
        link: String,
        /// The principal's entity type.
        principal: String,
    },
    /// A role link names a resource its template set does not take.
    #[error("role link {link}: a {scope} role does not link to {resource}")]
    RoleLinkScope {
        /// The link's id.
        link: String,
        /// The template set the link names.
        scope: &'static str,
        /// The resource it names.
        resource: String,
    },
    /// An organization's policy does not fit the schema.
    #[error("the policy does not fit the schema: {0}")]
    Validation(String),
    /// The entity store could not be read.
    #[error("the entity store could not be read")]
    EntityRead(#[source] Cause),
    /// An organization's policies could not be read.
    #[error("the policy store could not be read")]
    PolicyRead(#[source] Cause),
    /// An organization's policy does not join the system tier.
    #[error("policy {policy} does not join the system tier")]
    Composition {
        /// The policy's id.
        policy: String,
        /// Why it does not.
        #[source]
        source: Cause,
    },
}

/// The failure behind a variant, kept for the log at the boundary.
pub type Cause = Box<dyn std::error::Error + Send + Sync>;
