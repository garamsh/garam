//! Decides a request against the system tier, an organization's
//! policies and the entities they read, with the `cedar-policy` crate.
//! Any evaluation error is a deny (ADR-0137).

mod decision;
mod engine;
mod entity_source;
mod error;
mod policy_source;
mod role_link;
mod system_tier;

pub use decision::{Decision, Outcome, Reason};
pub use engine::{Engine, EntityRef, Evaluation, Policies};
pub use entity_source::{EntitySlice, EntitySource};
pub use error::{Cause, Error};
pub use policy_source::{OrgGrn, OrgPolicies, PolicySource};
pub use role_link::{Role, RoleLink, RoleScope};
