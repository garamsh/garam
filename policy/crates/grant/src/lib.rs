//! Holds an organization's policies: role assignments as links to the
//! system tier's templates, and `forbid` guardrails. Versions are
//! append-only, and only what the schema admits is written (ADR-0130).

mod body;
mod error;
mod postgres;

pub use body::Body;
pub use error::Error;
pub use postgres::{NewPolicy, PolicyStore, Status, Version};
