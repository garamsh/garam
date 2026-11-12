//! Reads the service manifests, the Cedar schema files under
//! `schema/` that declare each service (ADR-0143). It is the one
//! reader: the decision engine loads the merged schema from it, the
//! policy service reads the caller of each type from it, and the
//! generator and the checks read the rest.

mod catalogue;
mod error;
mod manifests;
mod service;

pub use catalogue::render_go;
pub use error::Error;
pub use manifests::Manifests;
pub use service::{Action, AppliesTo, EntityType, Service, Writer};
