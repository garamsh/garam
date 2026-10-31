use std::future::Future;

use cedar_policy::{Entities, EntityUid};

use crate::error::Cause;
use crate::policy_source::OrgGrn;

/// What one decision reads of the entity store.
#[derive(Debug, Clone)]
pub struct EntitySlice {
    /// The principal, the resource and everything each reaches, the
    /// resource's `Org` and `OrgMembers` among them (ADR-0137).
    pub entities: Entities,
    /// The organization `resource.org` names, absent when the store
    /// holds no such resource.
    pub org: Option<OrgGrn>,
}

/// Reads the entities a decision needs from the service's own store.
pub trait EntitySource {
    /// The slice for one principal and one resource.
    fn slice(
        &self,
        principal: &EntityUid,
        resource: &EntityUid,
    ) -> impl Future<Output = Result<EntitySlice, Cause>> + Send;
}
