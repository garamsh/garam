//! Holds an organization's policies: role assignments as links to the
//! system tier's templates, `forbid` guardrails, and `permit` policies
//! scoped to the organization. Versions are append-only, and only what
//! the schema admits is written (ADR-0130, ADR-0144).

mod body;
mod error;
mod link_name;
mod permit_right;
mod postgres;
mod step;

pub use body::{Body, BodyKind, Content};
pub use error::Error;
pub use link_name::{link_policy, names_link, reserved, scoped_link_policy};
pub use permit_right::PermitRight;
pub use postgres::{NewPolicy, PolicyStore, Status, Summary, Version};
pub use step::{Step, action_for};
