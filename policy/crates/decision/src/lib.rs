//! Decides a request against the system tier, an organization's
//! policies and the entities they read, with the `cedar-policy` crate.
//! Any evaluation error is a deny (ADR-0137).

mod decision;
mod engine;
mod error;
mod role_link;
mod system_tier;

pub use decision::{Decision, Outcome, Reason};
pub use engine::{Engine, EntityRef, Evaluation, Policies};
pub use error::Error;
pub use role_link::{Role, RoleLink, RoleScope};
