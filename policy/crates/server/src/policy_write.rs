//! The write of an organization's policies over gRPC (ADR-0146 item 3).
//! The platform alone calls it, and every call names the writer whose
//! action the service evaluates on the organization, read current,
//! before it stores anything.

use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{
    Cause, EntityRef, EntitySource, Evaluation, OrgGrn, OrgPolicies, Outcome, PolicySource, Role,
    RoleScope,
};
use grant::{
    Body, BodyKind, Content, NewPolicy, PermitRight, PolicyStore, Status, Step, Summary, action_for,
};
use manifest::is_slug;

use crate::caller::Principal;
use crate::policy_service::Service;
use crate::proto;
use crate::status::ServiceError;

/// Whether a write with no writer is one a membership makes. A role
/// link is written or removed from a membership with no writer: its
/// creation, its activation and its removal. Appending a version and
/// rolling back are an administrator's choices.
const fn unwritten(step: Step) -> bool {
    matches!(step, Step::Create | Step::Activate | Step::Remove)
}

/// Whether the step on this kind is a `permit`'s write, which asks
/// `iam:permit.write` as a right. A `permit`'s removal only narrows.
const fn needs_right(kind: BodyKind, step: Step) -> bool {
    matches!(kind, BodyKind::Permit) && !matches!(step, Step::Remove)
}

/// The caller and the subject a write is recorded as made by.
fn origin_of(principal: &Principal, writer: Option<&proto::Subject>) -> audit::Origin {
    audit::Origin {
        service: principal.sub(),
        subject: writer.map(|writer| writer.id.clone()),
    }
}

/// An action only the role holds, the evidence a writer holds it: the
/// owner alone may delete the organization, an administrator may update
/// it, and every member reads it.
const fn evidence_of(role: Role) -> &'static str {
    match role {
        Role::Owner => "iam:org.delete",
        Role::Admin => "iam:org.update",
        Role::Member => "iam:org.read",
    }
}

/// An organization's policies as a role is asked of: its role links
/// alone, so neither a guardrail nor a `permit` decides who holds one.
struct RolesOnly<'a, P>(&'a P);

impl<P: PolicySource + Sync> PolicySource for RolesOnly<'_, P> {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        let policies = self.0.active(org).await?;
        Ok(OrgPolicies {
            guardrails: Vec::new(),
            permits: Vec::new(),
            ..policies
        })
    }
}

impl<P, E> Service<P, E> {
    /// Gives the service the store its write RPCs write to. Without it
    /// they answer `UNAVAILABLE`.
    #[must_use]
    pub fn writing_to(mut self, store: PolicyStore) -> Self {
        self.store = Some(store);
        self
    }

    fn store(&self) -> Result<&PolicyStore, ServiceError> {
        self.store.as_ref().ok_or(ServiceError::WritesUnavailable)
    }
}

impl<P: PolicySource + Sync, E: EntitySource> Service<P, E> {
    /// Whether `writer` is allowed `action` on the organization, read
    /// from storage now. A denial is recorded where `asked_by` names
    /// the caller: the evidence a writer holds a role is the service's
    /// own probe of it, and is not.
    async fn allowed(
        &self,
        asked_by: Option<&Principal>,
        org: &OrgGrn,
        writer: &proto::Subject,
        action: &str,
        policies: &impl PolicySource,
    ) -> Result<bool, ServiceError> {
        let asked = Evaluation {
            subject: EntityRef {
                entity_type: &writer.r#type,
                id: &writer.id,
            },
            action,
            resource: EntityRef {
                entity_type: "Iam::Org",
                id: org.as_str(),
            },
            context: serde_json::json!({}),
        };
        let decision = self
            .engine
            .evaluate(policies, &self.entities, &asked)
            .await?;
        if let (Outcome::Deny, Some(principal)) = (decision.outcome, asked_by) {
            self.note_denial(principal, &asked, &decision).await;
        }
        Ok(decision.outcome == Outcome::Allow)
    }

    /// Settles whether the write may go on, and gives the right to
    /// write a `permit` where the step needs one. A writer holds each
    /// role a link names, and the action its step takes.
    async fn authorize(
        &self,
        principal: &Principal,
        org: &OrgGrn,
        writer: Option<&proto::Subject>,
        (kind, step): (BodyKind, Step),
        roles: &[Role],
    ) -> Result<Option<PermitRight>, ServiceError> {
        let Some(writer) = writer else {
            if kind == BodyKind::RoleLink && unwritten(step) {
                return Ok(None);
            }
            return Err(ServiceError::NoWriter);
        };
        for role in roles {
            let holds = self
                .allowed(
                    None,
                    org,
                    writer,
                    evidence_of(*role),
                    &RolesOnly(&self.policies),
                )
                .await?;
            if !holds {
                self.record_denial(
                    principal,
                    (&writer.id, action_for(kind, step), org.as_str()),
                    &[],
                )
                .await;
                return Err(ServiceError::RoleNotHeld {
                    writer: writer.id.clone(),
                    role: *role,
                });
            }
        }
        let action = action_for(kind, step);
        if !needs_right(kind, step) {
            if self
                .allowed(Some(principal), org, writer, action, &self.policies)
                .await?
            {
                return Ok(None);
            }
            return Err(ServiceError::WriterDenied {
                writer: writer.id.clone(),
                action,
            });
        }
        let subject = EntityRef {
            entity_type: &writer.r#type,
            id: &writer.id,
        };
        match PermitRight::ask(&self.engine, &self.policies, &self.entities, org, subject).await {
            Ok(right) => Ok(Some(right)),
            Err(grant::Error::PermitDenied { writer, decision }) => {
                self.note_denial(
                    principal,
                    &Evaluation {
                        subject,
                        action,
                        resource: EntityRef {
                            entity_type: "Iam::Org",
                            id: org.as_str(),
                        },
                        context: serde_json::json!({}),
                    },
                    &decision,
                )
                .await;
                Err(grant::Error::PermitDenied { writer, decision }.into())
            }
            Err(error) => Err(error.into()),
        }
    }

    /// A reader holds `action`, `iam:policy.read` or `iam:audit.read`,
    /// on the organization.
    async fn authorize_read(
        &self,
        principal: &Principal,
        org: &OrgGrn,
        reader: Option<&proto::Subject>,
        action: &'static str,
    ) -> Result<(), ServiceError> {
        let reader = reader.ok_or(ServiceError::NoWriter)?;
        if self
            .allowed(Some(principal), org, reader, action, &self.policies)
            .await?
        {
            return Ok(());
        }
        Err(ServiceError::WriterDenied {
            writer: reader.id.clone(),
            action,
        })
    }

    /// Who a write is recorded as: its writer, or the calling service
    /// for a write from a membership.
    fn author(origin: &audit::Origin) -> &str {
        origin.subject.as_deref().unwrap_or(&origin.service)
    }

    pub(crate) async fn create_policy(
        &self,
        principal: &Principal,
        request: &proto::CreatePolicyRequest,
    ) -> Result<proto::CreatePolicyResponse, ServiceError> {
        let origin = origin_of(principal, request.writer.as_ref());
        let store = self.reachable(principal)?.acting(origin.clone());
        let org = org_of(&request.org)?;
        let writer = request.writer.as_ref();
        let wanted = request.body.as_ref().ok_or(ServiceError::Missing("body"))?;
        let roles = roles_of(wanted)?;
        let right = self
            .authorize(
                principal,
                &org,
                writer,
                (kind_of(wanted)?, Step::Create),
                &roles,
            )
            .await?;
        let body = self.body_of(&org, wanted, right.as_ref())?;
        store
            .create(NewPolicy {
                grn: &request.policy,
                org: &org,
                name: &request.name,
                body,
                created_by: Self::author(&origin),
            })
            .await?;
        Ok(proto::CreatePolicyResponse {})
    }

    pub(crate) async fn append_policy_version(
        &self,
        principal: &Principal,
        request: &proto::AppendPolicyVersionRequest,
    ) -> Result<proto::AppendPolicyVersionResponse, ServiceError> {
        let origin = origin_of(principal, request.writer.as_ref());
        let store = &self.reachable(principal)?.acting(origin.clone());
        let org = org_of(&request.org)?;
        let writer = request.writer.as_ref();
        let wanted = request.body.as_ref().ok_or(ServiceError::Missing("body"))?;
        let existing = self
            .lookup(principal, store, &org, writer, &request.policy)
            .await?;
        if kind_of(wanted)? != existing.kind {
            return Err(grant::Error::KindChange(request.policy.clone()).into());
        }
        // Every role the policy has ever linked, and the one it would.
        let mut roles = self.roles_in(store, &org, &existing).await?;
        for role in roles_of(wanted)? {
            if !roles.contains(&role) {
                roles.push(role);
            }
        }
        let right = self
            .authorize(
                principal,
                &org,
                writer,
                (existing.kind, Step::Append),
                &roles,
            )
            .await?;
        let body = self.body_of(&org, wanted, right.as_ref())?;
        let version = store
            .append_version(&org, &request.policy, &body, Self::author(&origin))
            .await?;
        Ok(proto::AppendPolicyVersionResponse { version })
    }

    pub(crate) async fn activate_policy(
        &self,
        principal: &Principal,
        request: &proto::ActivatePolicyRequest,
    ) -> Result<proto::ActivatePolicyResponse, ServiceError> {
        let store = &self
            .reachable(principal)?
            .acting(origin_of(principal, request.writer.as_ref()));
        let org = org_of(&request.org)?;
        let writer = request.writer.as_ref();
        let existing = self
            .lookup(principal, store, &org, writer, &request.policy)
            .await?;
        let roles = self.roles_in(store, &org, &existing).await?;
        let right = self
            .authorize(
                principal,
                &org,
                writer,
                (existing.kind, Step::Activate),
                &roles,
            )
            .await?;
        // The version the writer was checked for is the one that goes
        // live: a roll-back in between makes this refuse.
        let checked = existing.default_version;
        match right {
            Some(right) => {
                store
                    .activate_permit_pinned(&org, &request.policy, checked, &right)
                    .await?;
            }
            None => {
                store
                    .activate_pinned(&org, &request.policy, checked)
                    .await?
            }
        }
        Ok(proto::ActivatePolicyResponse {})
    }

    pub(crate) async fn roll_back_policy(
        &self,
        principal: &Principal,
        request: &proto::RollBackPolicyRequest,
    ) -> Result<proto::RollBackPolicyResponse, ServiceError> {
        let store = &self
            .reachable(principal)?
            .acting(origin_of(principal, request.writer.as_ref()));
        let org = org_of(&request.org)?;
        let existing = self
            .lookup(
                principal,
                store,
                &org,
                request.writer.as_ref(),
                &request.policy,
            )
            .await?;
        let roles = self.roles_in(store, &org, &existing).await?;
        let right = self
            .authorize(
                principal,
                &org,
                request.writer.as_ref(),
                (existing.kind, Step::RollBack),
                &roles,
            )
            .await?;
        match right {
            Some(right) => {
                store
                    .roll_back_permit(&org, &request.policy, request.version, &right)
                    .await?;
            }
            None => {
                store
                    .roll_back(&org, &request.policy, request.version)
                    .await?;
            }
        }
        Ok(proto::RollBackPolicyResponse {})
    }

    pub(crate) async fn delete_policy(
        &self,
        principal: &Principal,
        request: &proto::DeletePolicyRequest,
    ) -> Result<proto::DeletePolicyResponse, ServiceError> {
        let store = &self
            .reachable(principal)?
            .acting(origin_of(principal, request.writer.as_ref()));
        let org = org_of(&request.org)?;
        let existing = self
            .lookup(
                principal,
                store,
                &org,
                request.writer.as_ref(),
                &request.policy,
            )
            .await?;
        // Every role any version links, since each one is gone with it.
        let roles = self.roles_in(store, &org, &existing).await?;
        self.authorize(
            principal,
            &org,
            request.writer.as_ref(),
            (existing.kind, Step::Remove),
            &roles,
        )
        .await?;
        store.delete(&org, &request.policy).await?;
        Ok(proto::DeletePolicyResponse {})
    }

    pub(crate) async fn get_policy(
        &self,
        principal: &Principal,
        request: &proto::GetPolicyRequest,
    ) -> Result<proto::GetPolicyResponse, ServiceError> {
        let store = self.reachable(principal)?;
        let org = org_of(&request.org)?;
        self.authorize_read(principal, &org, request.writer.as_ref(), "iam:policy.read")
            .await?;
        let summary = store.get(&org, &request.policy).await?;
        let versions = store.versions(&org, &request.policy).await?;
        Ok(proto::GetPolicyResponse {
            policy: Some(summary_of(&summary)),
            versions: versions
                .iter()
                .map(|version| proto::PolicyVersion {
                    version: version.version,
                    body: Some(body_to_proto(&version.body.content())),
                    created_by: version.created_by.clone(),
                })
                .collect(),
        })
    }

    pub(crate) async fn list_policies(
        &self,
        principal: &Principal,
        request: &proto::ListPoliciesRequest,
    ) -> Result<proto::ListPoliciesResponse, ServiceError> {
        let store = self.reachable(principal)?;
        let org = org_of(&request.org)?;
        self.authorize_read(principal, &org, request.writer.as_ref(), "iam:policy.read")
            .await?;
        let policies = store.list(&org).await?;
        Ok(proto::ListPoliciesResponse {
            policies: policies.iter().map(summary_of).collect(),
        })
    }

    /// An organization's records of policy writes and denials, read for
    /// a reader holding `iam:audit.read` on it. A read is not recorded,
    /// and a denied one is, like any denial. A page is at most
    /// [`MAX_AUDIT_PAGE`] records.
    pub(crate) async fn list_audit_records(
        &self,
        principal: &Principal,
        request: &proto::ListAuditRecordsRequest,
    ) -> Result<proto::ListAuditRecordsResponse, ServiceError> {
        platform_only(principal, "an audit read")?;
        let recorder = self
            .recorder
            .as_ref()
            .ok_or(ServiceError::AuditUnavailable)?;
        let org = org_of(&request.org)?;
        self.authorize_read(principal, &org, request.writer.as_ref(), "iam:audit.read")
            .await?;
        let after = match request.page_token.as_str() {
            "" => None,
            token => Some(token.parse::<i64>().map_err(|_| {
                ServiceError::Malformed("page_token is not one this service gave".to_owned())
            })?),
        };
        let size = match i64::from(request.page_size) {
            size if size <= 0 => MAX_AUDIT_PAGE,
            size => size.min(MAX_AUDIT_PAGE),
        };
        let page = recorder.list(org.as_str(), after, size).await?;
        Ok(proto::ListAuditRecordsResponse {
            records: page.records.iter().map(audit_record_of).collect(),
            next_page_token: page.next.map(|id| id.to_string()).unwrap_or_default(),
        })
    }

    /// The store, for a caller the write RPCs are for: the platform.
    /// A scoped caller is refused whole, reads included.
    fn reachable(&self, principal: &Principal) -> Result<&PolicyStore, ServiceError> {
        platform_only(principal, "a policy write")?;
        self.store()
    }

    /// The policy a write names. A writer is read as a reader first, so
    /// one without `iam:policy.read` is told nothing of whether the
    /// policy exists or what kind it is. A write with no writer is the
    /// platform's own and is told what it names.
    async fn lookup(
        &self,
        principal: &Principal,
        store: &PolicyStore,
        org: &OrgGrn,
        writer: Option<&proto::Subject>,
        policy: &str,
    ) -> Result<Summary, ServiceError> {
        if writer.is_some() {
            self.authorize_read(principal, org, writer, "iam:policy.read")
                .await?;
        }
        Ok(store.get(org, policy).await?)
    }

    /// Every role any version of a link policy names. Another kind
    /// names none.
    async fn roles_in(
        &self,
        store: &PolicyStore,
        org: &OrgGrn,
        policy: &Summary,
    ) -> Result<Vec<Role>, ServiceError> {
        if policy.kind != BodyKind::RoleLink {
            return Ok(Vec::new());
        }
        let mut roles = Vec::new();
        for stored in store.versions(org, &policy.grn).await? {
            if let Content::RoleLink { role, .. } = stored.body.content()
                && !roles.contains(&role)
            {
                roles.push(role);
            }
        }
        Ok(roles)
    }

    /// The body the request states, made through the checks only
    /// `grant` makes one by.
    fn body_of(
        &self,
        org: &OrgGrn,
        wanted: &proto::PolicyBody,
        right: Option<&PermitRight>,
    ) -> Result<Body, ServiceError> {
        use proto::policy_body::Body as Wire;
        match wanted.body.as_ref().ok_or(ServiceError::Missing("body"))? {
            Wire::RoleLink(link) => {
                let (role, scope) = (role_of(link.role)?, scope_of(link.scope)?);
                Ok(Body::role_link(
                    &self.engine,
                    org,
                    role,
                    scope,
                    uid_of(link.principal.as_ref(), "principal")?,
                    uid_of(link.resource.as_ref(), "resource")?,
                )?)
            }
            Wire::Guardrail(source) => Ok(Body::guardrail(&self.engine, org, source)?),
            Wire::Permit(source) => {
                let right = right.ok_or(ServiceError::NoWriter)?;
                Ok(Body::permit(&self.engine, right, source)?)
            }
        }
    }
}

/// The most records one page of the audit read holds.
const MAX_AUDIT_PAGE: i64 = 100;

/// Refuses a scoped caller whole: what is asked is the platform's.
fn platform_only(principal: &Principal, what: &'static str) -> Result<(), ServiceError> {
    if principal.caller().scoped() {
        return Err(ServiceError::OutOfScope(what));
    }
    Ok(())
}

/// A record as the contract carries it: a time is a `Timestamp`, and
/// what a record does not hold is empty or zero.
fn audit_record_of(record: &audit::Record) -> proto::AuditRecord {
    let at = |micros: i64| prost_types::Timestamp {
        seconds: micros.div_euclid(1_000_000),
        nanos: i32::try_from(micros.rem_euclid(1_000_000) * 1000).unwrap_or(0),
    };
    proto::AuditRecord {
        id: record.id,
        kind: match record.kind {
            audit::Kind::Write => proto::AuditRecordKind::Write,
            audit::Kind::Denial => proto::AuditRecordKind::Denial,
            audit::Kind::DenialOverflow => proto::AuditRecordKind::DenialOverflow,
        }
        .into(),
        first_at: Some(at(record.first_at)),
        last_at: Some(at(record.last_at)),
        count: record.count,
        service: record.service.clone().unwrap_or_default(),
        subject: record.subject.clone().unwrap_or_default(),
        action: record.action.clone().unwrap_or_default(),
        resource: record.resource.clone().unwrap_or_default(),
        outcome: record.outcome.clone().unwrap_or_default(),
        policy_ids: record.policy_ids.clone(),
        version_from: record.version_from.unwrap_or_default(),
        version_to: record.version_to.unwrap_or_default(),
        day: record
            .day
            .map(|seconds| prost_types::Timestamp { seconds, nanos: 0 }),
    }
}

/// The organization a request names: `grn:root/iam/org/<slug>`.
fn org_of(grn: &str) -> Result<OrgGrn, ServiceError> {
    let org = OrgGrn::new(grn);
    if org.as_str() == format!("grn:root/iam/org/{}", org.slug()) && is_slug(org.slug()) {
        return Ok(org);
    }
    Err(ServiceError::Malformed(format!(
        "{grn} is not an organization's GRN"
    )))
}

fn kind_of(body: &proto::PolicyBody) -> Result<BodyKind, ServiceError> {
    use proto::policy_body::Body as Wire;
    Ok(
        match body.body.as_ref().ok_or(ServiceError::Missing("body"))? {
            Wire::RoleLink(_) => BodyKind::RoleLink,
            Wire::Guardrail(_) => BodyKind::Guardrail,
            Wire::Permit(_) => BodyKind::Permit,
        },
    )
}

/// The role a link body grants, which a writer must hold.
fn roles_of(body: &proto::PolicyBody) -> Result<Vec<Role>, ServiceError> {
    use proto::policy_body::Body as Wire;
    match body.body.as_ref() {
        Some(Wire::RoleLink(link)) => Ok(vec![role_of(link.role)?]),
        _ => Ok(Vec::new()),
    }
}

fn role_of(wire: i32) -> Result<Role, ServiceError> {
    match proto::Role::try_from(wire) {
        Ok(proto::Role::Owner) => Ok(Role::Owner),
        Ok(proto::Role::Admin) => Ok(Role::Admin),
        Ok(proto::Role::Member) => Ok(Role::Member),
        Ok(proto::Role::Unspecified) | Err(_) => Err(ServiceError::Missing("role")),
    }
}

fn scope_of(wire: i32) -> Result<RoleScope, ServiceError> {
    match proto::RoleScope::try_from(wire) {
        Ok(proto::RoleScope::Organization) => Ok(RoleScope::Organization),
        Ok(proto::RoleScope::TeamOrProject) => Ok(RoleScope::TeamOrProject),
        Ok(proto::RoleScope::Unspecified) | Err(_) => Err(ServiceError::Missing("scope")),
    }
}

fn uid_of(
    entity: Option<&proto::EntityRef>,
    what: &'static str,
) -> Result<EntityUid, ServiceError> {
    let entity = entity.ok_or(ServiceError::Missing(what))?;
    let name = EntityTypeName::from_str(&entity.r#type)
        .map_err(|error| ServiceError::Malformed(error.to_string()))?;
    Ok(EntityUid::from_type_name_and_id(
        name,
        EntityId::new(&entity.id),
    ))
}

fn summary_of(summary: &Summary) -> proto::PolicySummary {
    proto::PolicySummary {
        policy: summary.grn.clone(),
        name: summary.name.clone(),
        status: match summary.status {
            Status::Draft => proto::PolicyStatus::Draft,
            Status::Active => proto::PolicyStatus::Active,
        }
        .into(),
        default_version: summary.default_version,
        kind: match summary.kind {
            BodyKind::RoleLink => proto::PolicyKind::RoleLink,
            BodyKind::Guardrail => proto::PolicyKind::Guardrail,
            BodyKind::Permit => proto::PolicyKind::Permit,
        }
        .into(),
    }
}

fn body_to_proto(content: &Content) -> proto::PolicyBody {
    use proto::policy_body::Body as Wire;
    let entity = |(r#type, id): &(String, String)| proto::EntityRef {
        r#type: r#type.clone(),
        id: id.clone(),
    };
    proto::PolicyBody {
        body: Some(match content {
            Content::RoleLink {
                role,
                scope,
                principal,
                resource,
            } => Wire::RoleLink(proto::RoleLink {
                role: match role {
                    Role::Owner => proto::Role::Owner,
                    Role::Admin => proto::Role::Admin,
                    Role::Member => proto::Role::Member,
                }
                .into(),
                scope: match scope {
                    RoleScope::Organization => proto::RoleScope::Organization,
                    RoleScope::TeamOrProject => proto::RoleScope::TeamOrProject,
                }
                .into(),
                principal: Some(entity(principal)),
                resource: Some(entity(resource)),
            }),
            Content::Guardrail(source) => Wire::Guardrail(source.clone()),
            Content::Permit(source) => Wire::Permit(source.clone()),
        }),
    }
}
