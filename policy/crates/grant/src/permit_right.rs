use decision::{Engine, EntityRef, EntitySource, Evaluation, OrgGrn, Outcome, PolicySource};

use crate::error::Error;

/// The right to write a `permit` for an organization: the answer of an
/// evaluation of `iam:policy-permit` on its `Org` for one writer
/// (ADR-0144 item 7). Only [`PermitRight::ask`] makes one, and only on
/// an allow, so a [`Body`](crate::Body) holding a `permit` was made by
/// a writer the engine allowed.
#[derive(Debug)]
pub struct PermitRight {
    org: OrgGrn,
    writer: String,
}

impl PermitRight {
    /// Evaluates `iam:policy-permit` on `org`'s `Org` for `writer`, a
    /// user or an agent, against the system tier, the organization's
    /// policies and its entities.
    pub async fn ask<P: PolicySource, E: EntitySource>(
        engine: &Engine,
        policies: &P,
        entities: &E,
        org: &OrgGrn,
        writer: EntityRef<'_>,
    ) -> Result<Self, Error> {
        let decision = engine
            .evaluate(
                policies,
                entities,
                &Evaluation {
                    subject: writer,
                    action: "iam:policy-permit",
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

    /// The organization the right was given on.
    pub(crate) fn org(&self) -> &OrgGrn {
        &self.org
    }

    /// The writer's GRN.
    pub(crate) fn writer(&self) -> &str {
        &self.writer
    }
}
