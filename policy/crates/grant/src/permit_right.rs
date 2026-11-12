use decision::{
    Cause, Engine, EntityRef, EntitySource, Evaluation, OrgGrn, OrgPolicies, Outcome, PolicySource,
};

use crate::error::Error;

/// The right to write a `permit` for an organization: the answer of an
/// evaluation of `iam:permit.write` on its `Org` for one writer
/// (ADR-0144 item 7). Only [`PermitRight::ask`] makes one, and only on
/// an allow, so a [`Body`](crate::Body) holding a `permit` was made by
/// a writer the engine allowed.
#[derive(Debug)]
pub struct PermitRight {
    org: OrgGrn,
    writer: String,
}

impl PermitRight {
    /// Evaluates `iam:permit.write` on `org`'s `Org` for `writer`, a
    /// user or an agent, against the system tier, the organization's
    /// role links and `forbid` guardrails and its entities. Its
    /// `permit`s are left out, so the right comes from the role alone:
    /// no `permit` confers it, and a guardrail can take it away.
    pub async fn ask<P: PolicySource + Sync, E: EntitySource>(
        engine: &Engine,
        policies: &P,
        entities: &E,
        org: &OrgGrn,
        writer: EntityRef<'_>,
    ) -> Result<Self, Error> {
        let decision = engine
            .evaluate(
                &WithoutPermits(policies),
                entities,
                &Evaluation {
                    subject: writer,
                    action: "iam:permit.write",
                    resource: EntityRef {
                        entity_type: "Iam::Org",
                        id: org.as_str(),
                    },
                    context: serde_json::json!({}),
                },
            )
            .await
            .map_err(Error::PermitEvaluation)?;
        match decision.outcome {
            Outcome::Allow => Ok(Self {
                org: org.clone(),
                writer: writer.id.to_owned(),
            }),
            Outcome::Deny => Err(Error::PermitDenied {
                writer: writer.id.to_owned(),
                decision,
            }),
        }
    }

    /// Refuses an organization other than the one the right was given
    /// on.
    pub(crate) fn check_org(&self, org: &OrgGrn) -> Result<(), Error> {
        if &self.org == org {
            return Ok(());
        }
        Err(Error::PermitRight)
    }

    /// The organization the right was given on.
    pub(crate) fn org(&self) -> &OrgGrn {
        &self.org
    }

    /// The writer's GRN.
    pub(crate) fn writer(&self) -> &str {
        &self.writer
    }
}

/// An organization's policies as the right is asked of: no `permit`.
struct WithoutPermits<'a, P>(&'a P);

impl<P: PolicySource + Sync> PolicySource for WithoutPermits<'_, P> {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        let policies = self.0.active(org).await?;
        Ok(OrgPolicies {
            permits: Vec::new(),
            ..policies
        })
    }
}
